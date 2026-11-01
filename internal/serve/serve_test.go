package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"observe/internal/model"
)

const specDoc = `<!-- specs:locked:abc1234 2026-01-01 type=spec -->

# Spec
## Scope
- The reader exposes ` + "`Open`" + ` for a repository.
- The cache lives in ` + "`internal/store`" + `.
`

// fixtureRepo builds a temporary repository: a locked spec, one Go file declaring
// Open, and the working-tree edit the tests classify.
func fixtureRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init")
	for name, body := range files {
		write(t, dir, name, body)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false", "commit", "-m", "initial")
	return dir
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// request builds a request the way a loopback browser sends it: the Host must be
// a loopback name, or the server refuses it (AC11).
func request(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Host = "127.0.0.1:4000"
	return req
}

// newServer wires a server over a real fixture repository.
func newServer(t *testing.T, dir, cachePath string) *Server {
	t.Helper()
	s, err := New(Options{RepoPath: dir, CachePath: cachePath})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// state reads the state route through the handler, which is the contract the UI
// consumes.
func state(t *testing.T, s *Server) State {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, request(http.MethodGet, "/api/state"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/state = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		OK    bool   `json:"ok"`
		Data  State  `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode state: %v: %s", err, rec.Body)
	}
	if !body.OK {
		t.Fatalf("state route refused: %s", body.Error)
	}
	return body.Data
}

// AC4: one payload carries every level of the drill-down — goal, its spec
// locator, the evidence reference, and the exact source line range.
func TestStateCarriesTheWholeDrillDown(t *testing.T) {
	dir := fixtureRepo(t, map[string]string{
		".agents/specs/spec.md": specDoc,
		"internal/repo/repo.go": "package repo\n\nfunc Open() {}\n",
	})
	write(t, dir, "internal/repo/repo.go", "package repo\n\nfunc Open() error {\n\treturn nil\n}\n")

	s := newServer(t, dir, "")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := state(t, s)

	if len(got.Goals) != 2 {
		t.Fatalf("goals = %d, want 2: %+v", len(got.Goals), got.Goals)
	}
	goal := got.Goals[0]
	if goal.SpecPath != ".agents/specs/spec.md" || len(goal.Heading) == 0 || goal.Label == "" {
		t.Fatalf("goal locator incomplete: %+v", goal)
	}
	if goal.Relation != "appears-to-support" {
		t.Fatalf("relation = %q, want appears-to-support", goal.Relation)
	}
	if len(goal.Evidence) == 0 {
		t.Fatal("no evidence on the classified goal")
	}
	ref := goal.Evidence[0]
	if ref.Path != "internal/repo/repo.go" || ref.Symbol != "Open" || ref.LineStart == 0 {
		t.Fatalf("evidence cannot be drilled to a line: %+v", ref)
	}
	if !goal.Inferred {
		t.Error("a diff-matched relation must stay marked inferred")
	}
	if goal.Verification != "unknown" {
		t.Errorf("verification = %q: a diff never verifies", goal.Verification)
	}
	if len(got.Diff.Files) != 1 || got.Diff.Files[0].Added == 0 {
		t.Fatalf("diff not carried: %+v", got.Diff)
	}
}

// AC7: no usable locked spec keeps browsing available and marks alignment
// unavailable, without inventing goals from other docs.
func TestNoLockedSpecKeepsBrowsingAndMarksAlignmentUnavailable(t *testing.T) {
	dir := fixtureRepo(t, map[string]string{
		"README.md": "# notes\n\n- the reader should open repositories\n",
		"main.go":   "package main\n",
	})
	s := newServer(t, dir, "")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := state(t, s)

	if got.Baseline.State != "missing" {
		t.Fatalf("baseline = %q, want missing", got.Baseline.State)
	}
	if len(got.Goals) != 0 {
		t.Fatalf("goals inferred without a locked spec: %+v", got.Goals)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, request(http.MethodGet, "/"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("page not served without a baseline: %d", rec.Code)
	}
}

// AC8: two locked specs leave the conflict unresolved and neither is preferred.
func TestConflictingLockedSpecsAreShownUnresolved(t *testing.T) {
	dir := fixtureRepo(t, map[string]string{
		".agents/specs/one.md": "<!-- specs:locked:aaa1111 2026-01-01 type=spec -->\n\n# One\n- goal one\n",
		".agents/specs/two.md": "<!-- specs:locked:bbb2222 2026-01-01 type=spec -->\n\n# Two\n- goal two\n",
	})
	s := newServer(t, dir, "")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := state(t, s)

	if got.Baseline.State != "conflicting" || len(got.Baseline.Specs) != 2 {
		t.Fatalf("baseline = %+v, want two conflicting specs", got.Baseline)
	}
	if len(got.Goals) != 0 {
		t.Fatalf("one spec was silently preferred: %+v", got.Goals)
	}
}

// AC9: a failed refresh keeps the last successful view, marks it stale with time
// and reason, and leaves retry available.
func TestFailedRefreshKeepsTheLastViewStaleWithReason(t *testing.T) {
	ok := Analyzed{
		Repository: "/repo",
		Snapshot: model.Snapshot{
			Generation: "gen-1",
			Freshness:  model.FreshnessCurrent,
			Assessments: []model.GoalAssessment{{
				Locator:      model.GoalLocator{SpecPath: "spec.md", Label: "a goal", HeadingChain: []string{"Scope"}},
				Relation:     model.RelationAppearsToSupport,
				Verification: model.VerificationUnknown,
			}},
		},
		Baseline: model.SpecBaseline{State: model.BaselineAvailable, SpecPaths: []string{"spec.md"}},
	}

	var fail bool
	s, err := New(Options{
		RepoPath: "/repo",
		Analysis: func(context.Context) (Analyzed, error) {
			if fail {
				return Analyzed{}, errors.New("repository unreadable")
			}
			return ok, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("the failing refresh reported success")
	}

	got := state(t, s)
	if got.Freshness != "stale" {
		t.Fatalf("freshness = %q, want stale", got.Freshness)
	}
	if got.Failure == nil || !strings.Contains(got.Failure.Reason, "unreadable") || got.Failure.At == "" {
		t.Fatalf("failure not reported with time and reason: %+v", got.Failure)
	}
	if len(got.Goals) != 1 {
		t.Fatalf("the last successful view was lost: %+v", got.Goals)
	}
	if got.Goals[0].Relation != "stale" {
		t.Fatalf("relation = %q: a stale result must not present its old status", got.Goals[0].Relation)
	}
	if got.Goals[0].Verification != "unknown" {
		t.Errorf("stale verification = %q", got.Goals[0].Verification)
	}
}

// AC9: with no prior success, the failure is shown and browsing still works.
func TestFailureWithoutAPriorSuccessShowsTheErrorAndKeepsThePage(t *testing.T) {
	s, err := New(Options{
		RepoPath: "/repo",
		Analysis: func(context.Context) (Analyzed, error) {
			return Analyzed{}, errors.New("not a git repository")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("the refresh reported success")
	}

	got := state(t, s)
	if got.Freshness != "failed" || got.Failure == nil {
		t.Fatalf("state = %+v, want a reported failure", got)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, request(http.MethodGet, "/"))
	if rec.Code != http.StatusOK {
		t.Fatalf("page unavailable after a failed first analysis: %d", rec.Code)
	}
}

// AC11: mutations need the per-launch token, and a cross-origin request is
// refused even with it.
func TestMutationsRequireTheTokenAndASameOriginRequest(t *testing.T) {
	s := newServer(t, fixtureRepo(t, map[string]string{"main.go": "package main\n"}), "")

	// do posts a mutation and reports whether the server accepted it.
	do := func(headers map[string]string) bool {
		req := request(http.MethodPost, "/api/refresh")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		var body struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v: %s", err, rec.Body)
		}
		return body.OK
	}

	if do(nil) {
		t.Error("a mutation without the token was accepted")
	}
	if do(map[string]string{tokenHeader: "wrong"}) {
		t.Error("a mutation with a wrong token was accepted")
	}
	if do(map[string]string{tokenHeader: s.Token(), "Origin": "http://evil.example"}) {
		t.Error("a cross-origin mutation was accepted")
	}
	if do(map[string]string{tokenHeader: s.Token(), "Sec-Fetch-Site": "cross-site"}) {
		t.Error("a cross-site mutation was accepted")
	}
	if !do(map[string]string{tokenHeader: s.Token()}) {
		t.Error("a same-origin mutation with the token was refused")
	}
}

// AC11: a non-loopback listen address is refused, so the app cannot be exposed to
// a network.
func TestListenRefusesANonLoopbackAddress(t *testing.T) {
	s := newServer(t, fixtureRepo(t, map[string]string{"main.go": "package main\n"}), "")
	for _, addr := range []string{"0.0.0.0:0", "192.0.2.1:8080", ":8080", "[::]:0"} {
		if _, err := s.Listen(addr); err == nil {
			t.Errorf("Listen accepted %q", addr)
		}
	}
}

// AC11: the page is served with a token, and the token is not guessable.
func TestPageCarriesAFreshTokenAndTheTokenIsUnguessable(t *testing.T) {
	s := newServer(t, fixtureRepo(t, map[string]string{"main.go": "package main\n"}), "")
	other := newServer(t, fixtureRepo(t, map[string]string{"main.go": "package main\n"}), "")

	if s.Token() == other.Token() || len(s.Token()) < 32 {
		t.Fatalf("tokens are not per-launch and unguessable: %q %q", s.Token(), other.Token())
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, request(http.MethodGet, "/"))
	if !strings.Contains(rec.Body.String(), s.Token()) {
		t.Fatal("the page does not carry the token it needs for mutations")
	}
}

// AC12: the clear action discards the disposable cache, and the next analysis
// simply rebuilds it.
func TestClearCacheDiscardsTheRecord(t *testing.T) {
	dir := fixtureRepo(t, map[string]string{
		".agents/specs/spec.md": specDoc,
		"internal/repo/repo.go": "package repo\n\nfunc Open() {}\n",
	})
	cachePath := filepath.Join(t.TempDir(), "cache", "analysis.db")
	s := newServer(t, dir, cachePath)

	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := state(t, s).Cache.State; got != "saved" {
		t.Fatalf("cache state = %q, want saved", got)
	}

	req := request(http.MethodPost, "/api/cache/clear")
	req.Header.Set(tokenHeader, s.Token())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear = %d: %s", rec.Code, rec.Body)
	}
	if got := state(t, s).Cache.State; got != "absent" {
		t.Fatalf("cache state after clear = %q, want absent", got)
	}

	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := state(t, s).Cache.State; got != "saved" {
		t.Fatalf("cache was not rebuilt: %q", got)
	}
}

// AC12 and the pipeline: an unchanged repository reuses the record, and a change
// invalidates it.
func TestPipelineReusesThenInvalidatesTheCache(t *testing.T) {
	dir := fixtureRepo(t, map[string]string{
		".agents/specs/spec.md": specDoc,
		"internal/repo/repo.go": "package repo\n\nfunc Open() {}\n",
	})
	cachePath := filepath.Join(t.TempDir(), "cache", "analysis.db")
	s := newServer(t, dir, cachePath)

	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := state(t, s).Cache.State; got != "saved" {
		t.Fatalf("first pass did not save: %q", got)
	}
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := state(t, s).Cache.State; got != "current" {
		t.Fatalf("second pass did not reuse the record: %q", got)
	}

	write(t, dir, "internal/repo/repo.go", "package repo\n\nfunc Open() error { return nil }\n")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := state(t, s)
	if got.Cache.State != "saved" {
		t.Fatalf("changed inputs did not recompute: %q", got.Cache.State)
	}
	if got.Goals[0].Relation != "appears-to-support" {
		t.Fatalf("relation after the edit = %q", got.Goals[0].Relation)
	}
}

// AC11 structurally: this package has no outbound client, so no request can leave
// the machine. The check reads the sources, which is the audit the spec asks for.
func TestPackageHasNoOutboundClient(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"http.Client", "http.Get(", "http.Post(", "net.Dial", "net.DialTimeout", "tls.Dial"}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range forbidden {
			if strings.Contains(string(body), needle) {
				t.Errorf("%s contains %q: the application must not make outbound requests", name, needle)
			}
		}
	}
}

// The project's construction is part of the view: modules, the modules they import,
// and the imports that resolve nowhere here. It survives a missing baseline,
// because how a project is built does not depend on a spec (AC7).
func TestStateCarriesTheProjectStructure(t *testing.T) {
	dir := fixtureRepo(t, map[string]string{
		"cmd/tool/main.go":      "package main\n\nimport \"observe/internal/repo\"\n\nfunc main() { repo.Open() }\n",
		"internal/repo/repo.go": "package repo\n\nimport \"net/http\"\n\nfunc Open() { _ = http.MethodGet }\n",
	})
	s := newServer(t, dir, "")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	st := state(t, s)
	if st.Baseline.State != "missing" {
		t.Fatalf("baseline = %q, want missing: the structure check must not need a spec", st.Baseline.State)
	}

	modules := map[string]Module{}
	for _, m := range st.Structure.Modules {
		modules[m.Dir] = m
	}
	if len(modules) != 2 {
		t.Fatalf("modules = %+v, want cmd/tool and internal/repo", st.Structure.Modules)
	}
	if got := modules["internal/repo"].Imports; len(got) != 1 || got[0] != "net/http" {
		t.Errorf("internal/repo imports = %v, want [net/http]", got)
	}
	if got := modules["cmd/tool"].DependsOn; len(got) != 1 || got[0] != "internal/repo" {
		t.Errorf("cmd/tool depends on %v, want [internal/repo]", got)
	}
	if got := modules["internal/repo"].Symbols; len(got) != 1 || got[0].Name != "Open" || got[0].Line != 5 {
		t.Errorf("internal/repo symbols = %+v, want Open at line 5", got)
	}
	if got := modules["internal/repo"].Files; len(got) != 1 || got[0] != "internal/repo/repo.go" {
		t.Errorf("internal/repo files = %v, want its one file", got)
	}

	want := Edge{From: "cmd/tool", To: "internal/repo", Kind: "import", Import: "observe/internal/repo"}
	found := false
	for _, e := range st.Structure.Edges {
		if e == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("edges = %+v, want one %+v", st.Structure.Edges, want)
	}
}
