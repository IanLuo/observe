package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docTreeLine matches a repository-tree bullet that names a directory.
var docTreeLine = regexp.MustCompile("^- `([a-z][a-z0-9-]*(?:/[a-z0-9-]+)*)/`")

// The architecture document is the reader's map of this repository, and until a
// test held it, the map listed a package that did not exist. A map that names a
// component which is not here, or hides one that is, is worse than no map.
//
// The check runs both ways over internal/: every package named is on disk, and
// every package on disk is named. Other entries are prose about the future
// (`docs/adr/` says "none currently exist"), so they are left alone.
func TestArchitectureTreeMatchesTheRepository(t *testing.T) {
	root := "../.."
	body, err := os.ReadFile(filepath.Join(root, ".agents", "specs", "architecture.md"))
	if err != nil {
		t.Fatalf("read architecture: %v", err)
	}

	documented := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		if m := docTreeLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			documented[m[1]] = true
		}
	}
	if len(documented) == 0 {
		t.Fatal("no directory bullets found; the tree section moved or changed shape")
	}

	for dir := range documented {
		if !strings.HasPrefix(dir, "internal/") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, dir)); err != nil {
			t.Errorf("architecture.md names %s, which is not in the repository", dir)
		}
	}

	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatalf("read internal/: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := "internal/" + entry.Name()
		if !documented[dir] {
			t.Errorf("%s exists but architecture.md does not list it", dir)
		}
	}
}
