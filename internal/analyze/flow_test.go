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
