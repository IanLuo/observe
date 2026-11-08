package serve

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The page is the product, and until now only a person had walked it: the
// drill-down, the refresh it asks for when it regains attention, and the panel
// resize have each broken once and been caught by hand.
//
// These tests drive a real browser against a real server. The browser is found, never
// downloaded; without one the test skips and says so. Nothing from the analysed
// repository is executed — the page reads what the server already analysed.

// chromePath finds a browser to drive.
func chromePath() string {
	if p := os.Getenv("OBSERVE_CHROME"); p != "" {
		return p
	}
	for _, candidate := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if p, err := exec.LookPath("chromium"); err == nil {
		return p
	}
	return ""
}

// browser returns a page-driving context, or skips the test.
func browser(t *testing.T) context.Context {
	t.Helper()

	path := chromePath()
	if path == "" {
		t.Skip("no Chrome or Chromium on this machine; set OBSERVE_CHROME to run")
	}

	alloc, stopAllocator := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(path),
			chromedp.WindowSize(1440, 900),
		)...)
	t.Cleanup(stopAllocator)

	ctx, stopBrowser := chromedp.NewContext(alloc)
	t.Cleanup(stopBrowser)
	return ctx
}

// browserFixture is a repository the page can drill into: a locked spec with checks,
// a module, a declaration, and a working-tree change.
func browserFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := fixtureRepo(t, map[string]string{
		".agents/specs/spec.md":      specDoc,
		"internal/repo/repo.go":      "package repo\n\n// Open reads the repository.\nfunc Open() error {\n\treturn nil\n}\n",
		"internal/store/store.go":    "package store\n\nfunc Save() {}\n",
		"internal/repo/repo_test.go": "package repo\n\n// covers: open-exists\nfunc TestOpen(t *testing.T) {}\n",
	})
	write(t, dir, "internal/repo/repo.go", "package repo\n\n// Open reads the repository.\nfunc Open() error {\n\treturn nil\n}\n\nfunc Close() {}\n")

	s := newServer(t, dir, "")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	url, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return dir, url
}

// AC4: the drill-down from a goal to the exact source line, in the rendered page.
func TestBrowserDrillsFromGoalToSource(t *testing.T) {
	_, url := browserFixture(t)
	ctx := browser(t)

	visit(ctx, t, url)

	// The goal view, then the first goal in it.
	click(ctx, t, `Array.from(document.querySelectorAll("button.mode")).find(b => b.textContent.trim() === "Goals").click()`)
	waitFor(ctx, t, `document.querySelectorAll("g.node-goal").length > 0`)

	click(ctx, t, `document.querySelectorAll("g.node-goal")[0].dispatchEvent(new MouseEvent("click", { bubbles: true }))`)
	waitFor(ctx, t, `(document.querySelector("#detail").textContent || "").includes("Checks")`)

	goal := text(ctx, t, `document.querySelector("#detail").textContent`)
	if !strings.Contains(goal, "open-exists") {
		t.Errorf("goal detail does not name its check: %q", first(goal, 200))
	}
	if !strings.Contains(goal, "Pass") && !strings.Contains(goal, "Violated") {
		t.Errorf("goal detail carries no verdict: %q", first(goal, 200))
	}

	// Down to the module, then to the file, then to the declaration line.
	click(ctx, t, `document.querySelector("#nav button.outline-module").click()`)
	waitFor(ctx, t, `(document.querySelector("#detail").textContent || "").includes("internal/repo")`)

	click(ctx, t, `document.querySelector("#nav .outline-files button.outline-file").click()`)
	waitFor(ctx, t, `(document.querySelector("#detail").textContent || "").includes("repo.go")`)
	if file := text(ctx, t, `document.querySelector("#detail").textContent`); !strings.Contains(file, "Open") {
		t.Errorf("file detail lists no declaration: %q", first(file, 200))
	}
}

// AC6: the page asks for a refresh when it regains attention, so a view left open
// follows the repository instead of showing what it saw when the tab was opened.
func TestBrowserRefreshesWhenItRegainsAttention(t *testing.T) {
	dir, url := browserFixture(t)
	ctx := browser(t)

	visit(ctx, t, url)
	waitFor(ctx, t, `!!document.querySelector("#header .meta")`)
	before := text(ctx, t, `document.querySelector("#header .meta").textContent`)

	// Change the repository while the page is not looking.
	write(t, dir, "internal/repo/extra.go", "package repo\n\nfunc Extra() {}\n")

	// Attention, not a click: the handler listens for focus and visibility.
	eval[bool](ctx, t, `window.dispatchEvent(new Event("focus"))`)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		after := text(ctx, t, `document.querySelector("#header .meta").textContent`)
		if after != before {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("the view did not refresh on attention: still %q", strings.TrimSpace(before))
}

// The detail panel keeps its width where the reader left it, and gives it back.
func TestBrowserResizesTheDetailPanel(t *testing.T) {
	_, url := browserFixture(t)
	ctx := browser(t)

	visit(ctx, t, url)
	waitFor(ctx, t, `!!document.querySelector("#gutter")`)

	width := func() float64 {
		return eval[float64](ctx, t, `document.querySelector("#detail").getBoundingClientRect().width`)
	}

	start := width()
	click(ctx, t, `document.querySelector("#gutter").dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true }))`)
	afterKey := width()
	if afterKey <= start {
		t.Fatalf("ArrowLeft did not widen the panel: %v -> %v", start, afterKey)
	}

	click(ctx, t, `document.querySelector("#gutter").dispatchEvent(new MouseEvent("dblclick", { bubbles: true }))`)
	if reset := width(); reset != start {
		t.Errorf("double click did not restore the default width: %v -> %v", start, reset)
	}
}

// visit loads the page.
func visit(ctx context.Context, t *testing.T, url string) {
	t.Helper()
	if _, err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		t.Fatalf("navigate: %v", err)
	}
}

// eval runs a JS expression in the page and decodes its value.
func eval[T any](ctx context.Context, t *testing.T, expr string) T {
	t.Helper()
	value, err := chromedp.Run(ctx, chromedp.Evaluate[T](expr))
	if err != nil {
		t.Fatalf("evaluate %s: %v", first(expr, 80), err)
	}
	return value
}

// click runs a JS expression for its effect.
func click(ctx context.Context, t *testing.T, expr string) {
	t.Helper()
	eval[bool](ctx, t, expr+`, true`)
}

// text reads a JS expression that yields a string.
func text(ctx context.Context, t *testing.T, expr string) string {
	t.Helper()
	return eval[string](ctx, t, expr)
}

// waitFor blocks until a JS expression is true, or fails the test.
func waitFor(ctx context.Context, t *testing.T, expr string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if eval[bool](ctx, t, `!!(`+expr+`)`) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", first(expr, 120))
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// AC18: the data model is a picture in the page, drawn from migrations, and a table
// drills into the columns and the migration that declared it.
func TestBrowserDrawsTheDataModelFromMigrations(t *testing.T) {
	dir := fixtureRepo(t, map[string]string{
		".agents/specs/spec.md":  specDoc,
		"db/migrations/0001.sql": "CREATE TABLE creator (id uuid PRIMARY KEY);\nCREATE TABLE offer (id uuid PRIMARY KEY, creator_id uuid REFERENCES creator(id));\n",
		"db/migrations/0002.sql": "ALTER TABLE offer ALTER COLUMN id TYPE text;\n",
		"internal/repo/repo.go":  "package repo\n\nfunc Open() {}\n",
	})
	s := newServer(t, dir, "")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	url, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	ctx := browser(t)
	visit(ctx, t, url)

	click(ctx, t, `Array.from(document.querySelectorAll("button.mode")).find(b => b.textContent.trim() === "Data").click()`)
	waitFor(ctx, t, `document.querySelectorAll("g.er-table").length === 2`)

	// A table the reader could not fully model is marked, never silently complete.
	waitFor(ctx, t, `document.querySelectorAll("g.er-table.partial").length === 1`)

	click(ctx, t, `Array.from(document.querySelectorAll("g.er-table")).find(g => (g.textContent || "").startsWith("offer")).dispatchEvent(new MouseEvent("click", { bubbles: true }))`)
	waitFor(ctx, t, `(document.querySelector("#detail").textContent || "").includes("creator_id")`)

	detail := text(ctx, t, `document.querySelector("#detail").textContent`)
	for _, want := range []string{"creator.id", "db/migrations/0001.sql", "ALTER COLUMN"} {
		if !strings.Contains(detail, want) {
			t.Errorf("table detail does not show %q: %q", want, first(detail, 400))
		}
	}
}

// AC19 at the picture: the flow map draws an edge only where the reader followed
// the whole chain, and both ends open their own detail. A route with no edge stays
// counted, never silently dropped.
func TestBrowserDrawsTheFlowMap(t *testing.T) {
	dir := fixtureRepo(t, map[string]string{
		".agents/specs/spec.md":  specDoc,
		"db/migrations/0001.sql": "CREATE TABLE plan_version (id uuid PRIMARY KEY, name text NOT NULL);\n",
		"apps/api/src/index.ts":  "import { Store } from './store.js';\nimport { createServer } from './server.js';\n\nconst store = new Store();\n\ncreateServer({ store });\n",
		"apps/api/src/store.ts":  "export class Store {\n  async createPlan(input) {\n    return client.query(`INSERT INTO plan_version (name) VALUES ($1)`, [input.name]);\n  }\n}\n",
		"apps/api/src/plans.ts":  "export function createPlans({ store }) {\n  return { async createPlan(input) { return store.createPlan(input); } };\n}\n",
		"apps/api/src/server.ts": "import { createPlans } from './plans.js';\n\nexport function createServer({ store }) {\n  const plans = createPlans({ store });\n  app.post('/v1/plans', async (request) => plans.createPlan(request.body));\n  app.get('/v1/health', async () => 'ok');\n}\n",
	})
	s := newServer(t, dir, "")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	url, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	ctx := browser(t)
	visit(ctx, t, url)

	// The outline separates the routes that reach a table from the ones that stop,
	// and names the stop from the analyzer's code, not from English.
	waitFor(ctx, t, `(document.querySelector("#nav").textContent || "").includes("/v1/health")`)
	nav := text(ctx, t, `document.querySelector("#nav").textContent`)
	for _, want := range []string{"Reaches a table", "Stops before a table", "no store call"} {
		if !strings.Contains(nav, want) {
			t.Errorf("outline does not show %q: %q", want, first(nav, 500))
		}
	}

	click(ctx, t, `Array.from(document.querySelectorAll("button.mode")).find(b => b.textContent.trim() === "Flows").click()`)
	waitFor(ctx, t, `document.querySelectorAll("g.flow-node.route").length === 1`)
	waitFor(ctx, t, `document.querySelectorAll("g.flow-node.table").length === 1`)
	waitFor(ctx, t, `document.querySelectorAll("path.flow-edge").length === 1`)

	// The route with no table is counted in the footer, not erased.
	foot := text(ctx, t, `document.querySelector(".map-foot").textContent`)
	for _, want := range []string{"1 of 2 routes", "other 1"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer does not say %q: %q", want, foot)
		}
	}

	// A table node opens the data model's own detail.
	click(ctx, t, `document.querySelector("g.flow-node.table").dispatchEvent(new MouseEvent("click", { bubbles: true }))`)
	waitFor(ctx, t, `(document.querySelector("#detail").textContent || "").includes("plan_version")`)

	// A route node opens the flow's hops.
	click(ctx, t, `document.querySelector("g.flow-node.route").dispatchEvent(new MouseEvent("click", { bubbles: true }))`)
	waitFor(ctx, t, `(document.querySelector("#detail").textContent || "").includes("store method")`)
	detail := text(ctx, t, `document.querySelector("#detail").textContent`)
	for _, want := range []string{"POST /v1/plans", "use case", "store.createPlan", "apps/api/src/store.ts"} {
		if !strings.Contains(detail, want) {
			t.Errorf("flow detail does not show %q: %q", want, first(detail, 400))
		}
	}
}
