package analyze

import (
	"strings"
	"testing"
)

// flowFixture is the shape of a real route, in miniature: a composition root that
// builds a store and passes it into a factory, a factory that returns use cases, a
// handler that calls one, and a store method that names a table in its SQL.
func flowFixture() []File {
	return []File{
		{Path: "apps/api/src/index.ts", Body: []byte(`import { PostgresOfferStore } from '@app/db';
import { createServer } from './server.js';

const offers = new PostgresOfferStore(pool);

createServer({ offers });
`)},
		{Path: "apps/api/src/server.ts", Body: []byte(`import { createOfferUseCases } from '@app/application/offers';

export function createServer({ offers }) {
  const offersUseCases = createOfferUseCases({ store: offers });
  app.post('/v1/plans', async (request, reply) => {
    return offersUseCases.createPlan(request.body);
  });
  app.get('/v1/items', async (request, reply) => {
    return offersUseCases.listItems();
  });
}
`)},
		{Path: "packages/application/src/offers.ts", Body: []byte(`export function createOfferUseCases({ store }) {
  return {
    async createPlan(input) {
      return store.createPlan(input);
    },
    async listItems() {
      return store.listItems();
    },
  };
}
`)},
		{Path: "packages/db/src/offers.ts", Body: []byte(`export class PostgresOfferStore {
  async createPlan(input) {
    return client.query(` + "`INSERT INTO plan_version (name) VALUES ($1)`" + `, [input.name]);
  }
  async listItems() {
    return client.query(` + "`SELECT id FROM sellable_item WHERE creator_id = $1`" + `, [1]);
  }
}
`)},
	}
}

// AC19: a route reaches the table its store names, through a factory argument and a
// composition-root constant, and every hop carries its file and line.
func TestFlowReachesTheTableThroughTheBindings(t *testing.T) {
	flows := RequestFlows(flowFixture())

	plan := findFlow(t, flows, "POST", "/v1/plans")
	if len(plan.Table) != 1 || plan.Table[0] != "plan_version" {
		t.Fatalf("POST /v1/plans tables = %v, want [plan_version]", plan.Table)
	}
	if plan.Unknown != "" {
		t.Errorf("a fully bound route is unknown: %q", plan.Unknown)
	}
	for _, hop := range plan.Hops {
		if hop.Path == "" || hop.Line == 0 {
			t.Errorf("hop %q has no file and line: %+v", hop.Name, hop)
		}
	}
	if got := hopNames(plan); got != "route,use case,store method,table" {
		t.Errorf("hops = %s", got)
	}
}

// AC19: a hop the reader cannot follow stops the flow there and says why. The route
// is not given a table it did not reach.
func TestFlowStopsAtAnUnboundHop(t *testing.T) {
	files := flowFixture()
	// The factory now stores its argument in a module-level variable, which the
	// reader does not follow: the binding is no longer "passed unchanged".
	files[2].Body = []byte(`let saved;
export function createOfferUseCases({ store }) {
  saved = store;
  return {
    async createPlan(input) {
      return saved.createPlan(input);
    },
    async listItems() {
      return store.listItems();
    },
  };
}
`)

	flows := RequestFlows(files)
	plan := findFlow(t, flows, "POST", "/v1/plans")
	if len(plan.Table) != 0 {
		t.Errorf("an unbound route still names tables %v", plan.Table)
	}
	if !strings.Contains(plan.Unknown, "factory") {
		t.Errorf("unknown = %q, want the factory binding named", plan.Unknown)
	}
}

// AC19: a route whose SQL names no literal table is shown without a table.
func TestFlowNeverGuessesATable(t *testing.T) {
	files := flowFixture()
	files[3].Body = []byte(`export class PostgresOfferStore {
  async listItems() {
    return client.query(sqlFor("items"));
  }
  async createPlan(input) {
    return client.query(sqlFor("plan"));
  }
}
`)
	flows := RequestFlows(files)
	items := findFlow(t, flows, "GET", "/v1/items")
	if len(items.Table) != 0 {
		t.Errorf("a dynamic query named tables %v", items.Table)
	}
}

func findFlow(t *testing.T, flows []Flow, method, path string) Flow {
	t.Helper()
	for _, f := range flows {
		if f.Method == method && f.Route == path {
			return f
		}
	}
	t.Fatalf("no flow for %s %s in %+v", method, path, flows)
	return Flow{}
}

func hopNames(f Flow) string {
	var names []string
	for _, h := range f.Hops {
		names = append(names, h.Name)
	}
	return strings.Join(names, ",")
}

// A lock clause is not a table: "FOR UPDATE OF order_row" locks a row that was
// already named by the FROM. The reader reported "of" and "order_row" as tables
// before this was caught on a real repository.
func TestFlowIgnoresALockClause(t *testing.T) {
	files := flowFixture()
	files[3].Body = []byte(`export class PostgresOfferStore {
  async createPlan(input) {
    return client.query(` + "`SELECT id FROM sellable_item WHERE id = $1 FOR UPDATE OF sellable_item`" + `, [1]);
  }
  async listItems() {
    return client.query(` + "`SELECT id FROM sellable_item`" + `, [1]);
  }
}
`)
	plan := findFlow(t, RequestFlows(files), "POST", "/v1/plans")
	if len(plan.Table) != 1 || plan.Table[0] != "sellable_item" {
		t.Errorf("tables = %v, want [sellable_item]", plan.Table)
	}
}

// A factory returns several objects, one per helper. The published one is the API,
// and reading a helper's object made every method look like it skipped the store.
func TestFlowReadsThePublishedObject(t *testing.T) {
	files := flowFixture()
	files[2].Body = []byte(`export function createOfferUseCases({ store }) {
  function planView(plan) {
    return { name: plan.name };
  }
  async function createPlan(input) {
    return store.createPlan(input);
  }
  async function listItems() {
    return store.listItems();
  }
  return { createPlan, listItems };
}
`)
	flows := RequestFlows(files)
	plan := findFlow(t, flows, "POST", "/v1/plans")
	if len(plan.Table) != 1 || plan.Table[0] != "plan_version" {
		t.Errorf("POST /v1/plans tables = %v, want [plan_version] (%s)", plan.Table, plan.Unknown)
	}
	items := findFlow(t, flows, "GET", "/v1/items")
	if len(items.Table) != 1 || items.Table[0] != "sellable_item" {
		t.Errorf("GET /v1/items tables = %v, want [sellable_item] (%s)", items.Table, items.Unknown)
	}
}

// A factory can take its store by position. The parameter names decide nothing:
// `createLedgerUseCases(ledger, policies)` binds its first argument, and the
// argument's own shape says whether it is a class.
func TestFlowBindsAPositionalFactoryArgument(t *testing.T) {
	files := flowFixture()
	files[1].Body = []byte(`import { createLedgerUseCases } from '@app/application/ledger';

export function createServer({ ledger, policies }) {
  const useCases = createLedgerUseCases(ledger, policies);
  app.post('/v1/authorize', async (request) => useCases.authorize(request.body));
}
`)
	files[2] = File{Path: "apps/api/src/ledger.ts", Body: []byte(`export function createLedgerUseCases(ledger, operationPolicies) {
  return {
    async authorize(input) {
      operationPolicies.check(input);
      return ledger.hold(input);
    },
  };
}
`)}
	files[3] = File{Path: "apps/api/src/store.ts", Body: []byte(`export class PostgresOfferStore {
  async hold(input) {
    return client.query(` + "`INSERT INTO entitlement_hold (units) VALUES ($1)`" + `, [input.units]);
  }
}
`)}
	files[0].Body = []byte(`import { PostgresOfferStore } from './store.js';
import { createServer } from './server.js';

const ledger = new PostgresOfferStore(pool);

createServer({ ledger, policies: new PolicyReader() });
`)
	flow := findFlow(t, RequestFlows(files), "POST", "/v1/authorize")
	if len(flow.Table) != 1 || flow.Table[0] != "entitlement_hold" {
		t.Fatalf("tables = %v, unknown = %q", flow.Table, flow.Unknown)
	}
	// The store call is the one on the class: `ledger.hold`, not `operationPolicies.check`.
	if flow.Hops[2].Detail != "ledger.hold" {
		t.Errorf("store hop = %q, want ledger.hold", flow.Hops[2].Detail)
	}
}

// A handler that calls a store itself has no use case. The table is still reached,
// and the hop list says so rather than inventing a use case.
func TestFlowFollowsADirectStoreCall(t *testing.T) {
	files := flowFixture()
	files[1].Body = []byte(`export function createServer({ orders }) {
  app.get('/v1/orders', async (request) => {
    return orders.listOrders(request.query.limit);
  });
}
`)
	files[0].Body = []byte(`import { PostgresOrderStore } from './store.js';
import { createServer } from './server.js';

const orders = new PostgresOrderStore(pool);

createServer({ orders });
`)
	files[3] = File{Path: "apps/api/src/store.ts", Body: []byte(`export class PostgresOrderStore {
  async listOrders(limit) {
    return client.query(` + "`SELECT id FROM commerce_order LIMIT $1`" + `, [limit]);
  }
}
`)}
	flow := findFlow(t, RequestFlows(files), "GET", "/v1/orders")
	if len(flow.Table) != 1 || flow.Table[0] != "commerce_order" {
		t.Fatalf("tables = %v, unknown = %q", flow.Table, flow.Unknown)
	}
	for _, hop := range flow.Hops {
		if hop.Name == "use case" {
			t.Errorf("a direct store call has no use case hop: %+v", flow.Hops)
		}
	}
}

// A call site can pass some arguments and not others. Each argument keeps its
// position, so an argument this reader cannot name never slides the next one onto
// the wrong parameter.
func TestPositionalBindingSurvivesUnnamedArguments(t *testing.T) {
	files := flowFixture()
	files[1].Body = []byte(`import { createLedgerUseCases } from '@app/application/ledger';

export function createServer({ ledger }) {
  const useCases = createLedgerUseCases(new PolicyReader(pool), ledger);
  app.post('/v1/authorize', async (request) => useCases.authorize(request.body));
}
`)
	files[2] = File{Path: "apps/api/src/ledger.ts", Body: []byte(`export function createLedgerUseCases(operationPolicies, ledger) {
  return {
    async authorize(input) {
      return ledger.hold(input);
    },
  };
}
`)}
	files[3] = File{Path: "apps/api/src/store.ts", Body: []byte(`export class PostgresOfferStore {
  async hold(input) {
    return client.query(` + "`INSERT INTO entitlement_hold (units) VALUES ($1)`" + `, [input.units]);
  }
}
`)}
	files[0].Body = []byte(`import { PostgresOfferStore } from './store.js';
import { createServer } from './server.js';

const ledger = new PostgresOfferStore(pool);

createServer({ ledger });
`)
	flow := findFlow(t, RequestFlows(files), "POST", "/v1/authorize")
	if len(flow.Table) != 1 || flow.Table[0] != "entitlement_hold" {
		t.Fatalf("tables = %v, unknown = %q", flow.Table, flow.Unknown)
	}
}

// A use case can call an argument that is not a store before it reaches one: a
// policy reader, a clock. The store is the call whose method names a table, not
// whichever argument happens to be called first.
func TestFlowPrefersTheArgumentThatNamesATable(t *testing.T) {
	files := flowFixture()
	files[1].Body = []byte(`import { createOfferUseCases } from '@app/application/offers';

export function createServer({ offers, policies }) {
  const offersUseCases = createOfferUseCases({ store: offers, policies });
  app.post('/v1/plans', async (request) => offersUseCases.createPlan(request.body));
}
`)
	files[2].Body = []byte(`export function createOfferUseCases({ store, policies }) {
  return {
    async createPlan(input) {
      policies.check(input);
      return store.createPlan(input);
    },
  };
}
`)
	files[0].Body = []byte(`import { PostgresOfferStore, PolicyReader } from '@app/db';
import { createServer } from './server.js';

const offers = new PostgresOfferStore(pool);

createServer({ offers, policies: new PolicyReader(pool) });
`)
	flow := findFlow(t, RequestFlows(files), "POST", "/v1/plans")
	if len(flow.Table) != 1 || flow.Table[0] != "plan_version" {
		t.Fatalf("tables = %v, unknown = %q", flow.Table, flow.Unknown)
	}
	if flow.Hops[2].Detail != "store.createPlan" {
		t.Errorf("store hop = %q, want store.createPlan", flow.Hops[2].Detail)
	}
}

// A call site can leave an argument as an expression instead of a name:
// createServer(new PostgresLedger(pool)). The flow stops there, and the reason
// names that expression so the reader knows what to look at. Decided: this shape
// is not a binding.
func TestFlowNamesAnExpressionArgumentAsTheReason(t *testing.T) {
	files := flowFixture()
	files[0].Body = []byte(`import { PostgresOfferStore } from '@app/db';
import { createServer } from './server.js';

createServer({ offers: new PostgresOfferStore(pool) });
`)
	flow := findFlow(t, RequestFlows(files), "POST", "/v1/plans")
	if len(flow.Table) != 0 {
		t.Fatalf("tables = %v, want none", flow.Table)
	}
	if !strings.Contains(flow.Unknown, "passes an expression") || !strings.Contains(flow.Unknown, "offers") {
		t.Errorf("unknown = %q, want it to name the expression argument", flow.Unknown)
	}
}

// A stop is a code as well as a sentence. The reader groups flows by the code, so
// the vocabulary is fixed here rather than parsed out of English later.
func TestFlowStopsCarryACode(t *testing.T) {
	cases := []struct {
		name string
		file int
		body string
		want string
		look string
	}{
		{"no store call", 1, `export function createServer({ store }) {
  app.get('/v1/ping', async () => 'ok');
}
`, StopNoStoreCall, "GET /v1/ping"},
		{"expression argument", 0, "", StopFactoryArgument, ""},
		{"published object without the method", 2, `export function createOfferUseCases({ store }) {
  return { async listItems() { return store.listItems(); } };
}
`, StopFactoryMethod, ""},
		{"no literal table", 3, `export class PostgresOfferStore {
  async createPlan(input) {
    return client.query('INSERT INTO ' + input.table + ' VALUES ($1)', [1]);
  }
  async listItems() {
    return client.query('SELECT id FROM sellable_item', [1]);
  }
}
`, StopNoTable, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := flowFixture()
			if c.file == 0 {
				files[0].Body = []byte(`import { PostgresOfferStore } from '@app/db';
import { createServer } from './server.js';

createServer({ offers: new PostgresOfferStore(pool) });
`)
			} else if c.body != "" {
				files[c.file].Body = []byte(c.body)
			}
			method, route := "POST", "/v1/plans"
			if c.look != "" {
				method, route = "GET", "/v1/ping"
			}
			flow := findFlow(t, RequestFlows(files), method, route)
			if flow.Stop != c.want {
				t.Errorf("stop = %q, want %q (%s)", flow.Stop, c.want, flow.Unknown)
			}
			if flow.Unknown == "" {
				t.Error("a stopped flow carries no sentence")
			}
		})
	}

	// A resolved flow carries no stop code.
	flow := findFlow(t, RequestFlows(flowFixture()), "POST", "/v1/plans")
	if flow.Stop != "" {
		t.Errorf("a resolved flow has stop %q", flow.Stop)
	}
}

// A handler that calls a dependency the server receives is not a handler that
// calls nothing. The two stops must be told apart: the reason names the step that
// stopped the flow, and "no store call" for a route that does call a use case is a
// wrong reason. AC19 requires the step, not a convenient label.
func TestFlowNamesTheDependencyItCannotBind(t *testing.T) {
	dependencyFixture := func(handlerBody string) []File {
		return []File{
			{Path: "apps/api/src/index.ts", Body: []byte(`import { createServer } from './server.js';

createServer({ offers, notifications: createNotificationUseCases({ store: new PostgresSettingsStore(pool) }) });
`)},
			{Path: "apps/api/src/server.ts", Body: []byte(`export function createServer({ offers, notifications }) {
  app.put('/v1/notifications', async (request, reply) => {
` + handlerBody + `
  });
}
`)},
		}
	}

	t.Run("calls an injected use case", func(t *testing.T) {
		flow := findFlow(t, RequestFlows(dependencyFixture(`    return notifications.set(request.body);`)), "PUT", "/v1/notifications")
		if flow.Stop != StopUnboundDependency {
			t.Errorf("stop = %q, want %q (%s)", flow.Stop, StopUnboundDependency, flow.Unknown)
		}
		if !strings.Contains(flow.Unknown, "notifications.set") {
			t.Errorf("the reason does not name the call: %q", flow.Unknown)
		}
	})

	// The handler's own request and reply are not dependencies the server receives,
	// so a handler that only answers still reports that it calls no store.
	t.Run("only answers", func(t *testing.T) {
		flow := findFlow(t, RequestFlows(dependencyFixture(`    return reply.header('x', 'y').send({ ok: true });`)), "PUT", "/v1/notifications")
		if flow.Stop != StopNoStoreCall {
			t.Errorf("stop = %q, want %q (%s)", flow.Stop, StopNoStoreCall, flow.Unknown)
		}
	})
}
