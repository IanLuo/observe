package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const specDoc = `<!-- specs:locked:abc1234 2026-01-01 type=spec -->

# Spec
- first goal
`

func TestOpenRejectsDirectoryThatIsNotAWorkingTree(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("Open accepted a directory that is not a git working tree")
	}
}

func TestLockedSpecsFindsOnlyMarkedTrackedMarkdown(t *testing.T) {
	dir := newRepo(t, map[string]string{
		".agents/specs/spec.md": specDoc,
		"design.md":             "<!-- design:locked:def5678 2026-01-02 -->\n\n# Design\n",
		// A marker that is not the first non-empty line is a quotation, not a lock.
		"quoted.md": "# Notes\n<!-- specs:locked:aaa1111 2026-01-01 type=spec -->\n",
		"notes.txt": "<!-- specs:locked:bbb2222 2026-01-01 type=spec -->\n",
	})

	r := openRepo(t, dir)
	specs, err := r.LockedSpecs()
	if err != nil {
		t.Fatal(err)
	}

	if got, want := len(specs), 2; got != want {
		t.Fatalf("found %d locked specs, want %d: %+v", got, want, specs)
	}
	if specs[0].Path != ".agents/specs/spec.md" || specs[1].Path != "design.md" {
		t.Fatalf("specs not sorted by path: %q, %q", specs[0].Path, specs[1].Path)
	}
	if specs[0].Lock.Type != "spec" || specs[1].Lock.Type != "" {
		t.Fatalf("wrong lock types: %+v", []Lock{specs[0].Lock, specs[1].Lock})
	}
	if specs[0].Lock.Family != "specs" || specs[1].Lock.Family != "design" {
		t.Fatalf("wrong lock families: %+v", []Lock{specs[0].Lock, specs[1].Lock})
	}
	if len(specs[0].Hash) != 64 {
		t.Fatalf("content hash is not hex sha256: %q", specs[0].Hash)
	}
}

func TestReadFileRefusesPathsOutsideTheRepository(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.txt": "a"})
	r := openRepo(t, dir)

	for _, path := range []string{"", ".", "..", "../a.txt", "/etc/passwd"} {
		if _, err := r.ReadFile(path); err == nil {
			t.Fatalf("ReadFile(%q) was allowed", path)
		}
	}
	if _, err := r.ReadFile("a.txt"); err != nil {
		t.Fatalf("ReadFile(a.txt): %v", err)
	}
}

func TestChangesHashTracksTheSelectedDiff(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.txt": "a\n"})
	r := openRepo(t, dir)

	changes, before, err := r.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 || before == "" {
		t.Fatalf("clean tree: changes=%+v hash=%q", changes, before)
	}

	writeFile(t, dir, "a.txt", "a changed\n")
	changes, after, err := r.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "a.txt" {
		t.Fatalf("changes = %+v, want [a.txt]", changes)
	}
	if after == before {
		t.Fatal("diff hash did not change after the working tree changed")
	}
}

// Changes attributes the diff to each changed path, including the content on both
// sides, so a change can be judged against the code it touched.
func TestChangesAttributesTheDiffToEachPath(t *testing.T) {
	dir := newRepo(t, map[string]string{
		"a.txt": "a\n",
		"b.txt": "b\n",
	})
	writeFile(t, dir, "a.txt", "a changed\n")
	writeFile(t, dir, "untracked.txt", "new\n")

	r := openRepo(t, dir)
	changes, hash, err := r.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("empty diff hash")
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v, want two paths", changes)
	}
	if changes[0].Path != "a.txt" || changes[1].Path != "untracked.txt" {
		t.Fatalf("changes are not sorted by path: %q, %q", changes[0].Path, changes[1].Path)
	}

	a := changes[0]
	if !strings.Contains(a.Diff, "-a") || !strings.Contains(a.Diff, "+a changed") {
		t.Errorf("diff not attributed to a.txt: %q", a.Diff)
	}
	if string(a.Old) != "a\n" || string(a.New) != "a changed\n" {
		t.Errorf("wrong sides for a.txt: old=%q new=%q", a.Old, a.New)
	}

	// An untracked path has no HEAD version and no working-tree diff; its body is
	// the whole change.
	u := changes[1]
	if u.Status != "??" || u.Old != nil || u.Diff != "" {
		t.Errorf("untracked change = %+v", u)
	}
	if string(u.New) != "new\n" {
		t.Errorf("untracked body = %q", u.New)
	}
}

func TestChangesReportsADeletedFileWithNoNewSide(t *testing.T) {
	dir := newRepo(t, map[string]string{"gone.txt": "bye\n"})
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	changes, _, err := openRepo(t, dir).Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %+v", changes)
	}
	if changes[0].New != nil {
		t.Errorf("deleted file has a new side: %q", changes[0].New)
	}
	if string(changes[0].Old) != "bye\n" {
		t.Errorf("old side = %q, want the HEAD content", changes[0].Old)
	}
}

// TestReadOnlySessionLeavesRepositoryUnchanged is AC10: a complete read session
// leaves tracked/untracked state and file hashes unchanged. It includes .git, so
// an index refresh during `status` would show up here.
func TestReadOnlySessionLeavesRepositoryUnchanged(t *testing.T) {
	dir := newRepo(t, map[string]string{
		".agents/specs/spec.md": specDoc,
		"main.go":               "package main\n",
	})
	writeFile(t, dir, "untracked.txt", "untracked\n")

	before := snapshotTree(t, dir)

	r := openRepo(t, dir)
	if _, err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.TrackedFiles(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Changes(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LockedSpecs(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFile("main.go"); err != nil {
		t.Fatal(err)
	}

	after := snapshotTree(t, dir)
	if !reflect.DeepEqual(before, after) {
		for path, sum := range after {
			if before[path] != sum {
				t.Errorf("changed by a read-only session: %s", path)
			}
		}
		for path := range before {
			if _, ok := after[path]; !ok {
				t.Errorf("removed by a read-only session: %s", path)
			}
		}
		t.Fatal("a read-only session modified the repository")
	}
}

func TestCommitIsEmptyForARepositoryWithoutCommits(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init")
	r := openRepo(t, dir)

	commit, err := r.Commit()
	if err != nil {
		t.Fatal(err)
	}
	if commit != "" {
		t.Fatalf("commit = %q, want empty", commit)
	}
	if _, _, err := r.Changes(); err != nil {
		t.Fatalf("Changes on an unborn branch: %v", err)
	}
}

// newRepo creates a temporary repository with files committed.
func newRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init")
	for name, body := range files {
		writeFile(t, dir, name, body)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false", "commit", "-m", "initial")
	return dir
}

func openRepo(t *testing.T, dir string) *Repo {
	t.Helper()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Isolate from the machine's git configuration, which may enable signing.
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// snapshotTree maps every regular file under root, including .git, to its hash.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
