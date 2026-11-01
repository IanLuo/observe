package serve

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// AC11: the UI is served from the binary, so a machine with no network still gets
// a working page. The bundle must also carry no remote reference and no
// markup-parsing sink.
func TestEmbeddedAssetsAreSelfContained(t *testing.T) {
	s := newServer(t, fixtureRepo(t, map[string]string{"main.go": "package main\n"}), "")

	serve := func(path string) (int, string, string) {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, request(http.MethodGet, path))
		return rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()
	}

	code, ctype, page := serve("/")
	if code != http.StatusOK || !strings.Contains(ctype, "text/html") {
		t.Fatalf("page = %d %q", code, ctype)
	}
	if !strings.Contains(page, `src="/app.js"`) || !strings.Contains(page, `href="/app.css"`) {
		t.Fatal("the shell does not load the embedded assets")
	}
	if strings.Contains(page, "<script>") {
		t.Error("the shell carries an inline script; the bundle belongs in app.js")
	}

	code, ctype, js := serve("/app.js")
	if code != http.StatusOK || !strings.Contains(ctype, "text/javascript") || len(js) < 1000 {
		t.Fatalf("app.js = %d %q %d bytes", code, ctype, len(js))
	}
	// XML namespace URIs are identifiers, not requests; everything else that
	// reaches the network is refused.
	bundle := strings.ReplaceAll(js, "http://www.w3.org/2000/svg", "")
	bundle = strings.ReplaceAll(bundle, "http://www.w3.org/1999/xhtml", "")
	for _, remote := range []string{"http://", "https://", "//cdn.", "@import", "WebSocket", "XMLHttpRequest", "EventSource", "sendBeacon"} {
		if strings.Contains(bundle, remote) {
			t.Errorf("app.js references %q: the UI must not reach the network", remote)
		}
	}
	if strings.Contains(js, "innerHTML") {
		t.Error("app.js assigns innerHTML: repository text would be parsed as markup")
	}

	code, ctype, css := serve("/app.css")
	if code != http.StatusOK || !strings.Contains(ctype, "text/css") || !strings.Contains(css, "--accent") {
		t.Fatalf("app.css = %d %q", code, ctype)
	}

	// The mux cleans or rejects a traversal; either is fine, a served file is not.
	if code, _, _ := serve("/app.js/../../go.mod"); code == http.StatusOK {
		t.Errorf("traversal attempt was served (%d)", code)
	}
	if code, _, _ := serve("/nope.js"); code != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404", code)
	}
}

// The embedded bundle must be what web/ builds today, or the page served in
// production is not the page in the repository. The check is skipped when the
// toolchain is absent, so `go test` outside the dev shell still runs.
func TestEmbeddedBundleMatchesWebSources(t *testing.T) {
	for _, tool := range []string{"esbuild", "tsc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH: bundle drift unchecked (it is in the nix dev shell)", tool)
		}
	}

	web, err := filepath.Abs("../../web")
	if err != nil {
		t.Fatal(err)
	}
	check := exec.Command("tsc", "--noEmit", "-p", "tsconfig.json")
	check.Dir = web
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("web/ does not type-check: %v: %s", err, out)
	}

	out := filepath.Join(t.TempDir(), "app.js")
	build := exec.Command("esbuild", "main.ts", "--bundle", "--minify",
		"--target=es2022", "--format=iife", "--outfile="+out,
		"--banner:js="+"/* generated from web/ by web/build.sh — edit web/, not this file */")
	build.Dir = web
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("esbuild: %v: %s", err, output)
	}

	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(static, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("static/app.js is stale: run web/build.sh (%d bytes embedded, %d bytes built)", len(got), len(want))
	}
}
