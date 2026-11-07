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

	changes, before, _, err := r.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 || before == "" {
		t.Fatalf("clean tree: changes=%+v hash=%q", changes, before)
	}

	writeFile(t, dir, "a.txt", "a changed\n")
	changes, after, _, err := r.Changes()
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
	changes, hash, _, err := r.Changes()
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

	// An untracked path has no HEAD version, so its whole content is the addition:
	// the diff is the file, not an empty panel.
	u := changes[1]
	if u.Status != "??" || u.Old != nil {
		t.Errorf("untracked change = %+v", u)
	}
	if !strings.Contains(u.Diff, "+new") {
		t.Errorf("untracked diff does not show the added line: %q", u.Diff)
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

	changes, _, _, err := openRepo(t, dir).Changes()
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
	if _, _, _, err := r.Changes(); err != nil {
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
	if _, _, _, err := r.Changes(); err != nil {
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

// A nested worktree or submodule is an index entry whose on-disk form is a
// directory. Reading it as a file fails with "is a directory", so it is not
// tracked source.
func TestTrackedFilesOmitsGitlinks(t *testing.T) {
	dir := newRepo(t, map[string]string{"main.go": "package main\n"})

	// A gitlink without a .gitmodules file: exactly what a nested worktree leaves
	// in the index.
	gitIn(t, dir, "update-index", "--add", "--cacheinfo",
		"160000,"+strings.Repeat("a", 40)+",.claude/worktrees/agent-1")
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "worktrees", "agent-1"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths, err := openRepo(t, dir).TrackedFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasPrefix(path, ".claude/") {
			t.Fatalf("a gitlink was listed as a tracked file: %s", paths)
		}
	}
	if len(paths) == 0 {
		t.Fatal("the regular file disappeared with the gitlink")
	}
}

// An untracked nested repository appears in the status listing as a directory
// entry. It has no line diff and cannot be read as a file, so Changes reports it
// as skipped instead of failing.
func TestChangesSkipsADirectoryEntry(t *testing.T) {
	dir := newRepo(t, map[string]string{"main.go": "package main\n"})
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "worktrees", "agent-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "--quiet", filepath.Join(dir, ".claude", "worktrees", "agent-1"))

	changes, _, skipped, err := openRepo(t, dir).Changes()
	if err != nil {
		t.Fatalf("one directory entry failed the pass: %v", err)
	}
	if len(skipped) != 1 || skipped[0] != ".claude/worktrees/agent-1" {
		t.Fatalf("skipped = %v, want the nested worktree", skipped)
	}
	for _, change := range changes {
		if strings.HasPrefix(change.Path, ".claude/") {
			t.Fatalf("a directory was returned as a file change: %+v", change)
		}
	}
}

// Work in progress is the normal case: a new source file exists on disk before it
// is committed. Files therefore includes untracked, non-ignored paths — and still
// excludes what .gitignore declares as not this project.
func TestFilesIncludesUntrackedAndRespectsIgnore(t *testing.T) {
	dir := newRepo(t, map[string]string{"main.go": "package main\n", ".gitignore": "dist/\n"})
	writeFile(t, dir, "src/new.go", "package src\n")
	writeFile(t, dir, "dist/bundle.js", "built\n")

	paths, err := openRepo(t, dir).Files()
	if err != nil {
		t.Fatal(err)
	}
	var sawNew, sawBuilt bool
	for _, path := range paths {
		switch path {
		case "src/new.go":
			sawNew = true
		case "dist/bundle.js":
			sawBuilt = true
		}
	}
	if !sawNew {
		t.Errorf("an untracked source file is not part of the project: %v", paths)
	}
	if sawBuilt {
		t.Errorf("an ignored build output was read: %v", paths)
	}
}

// Churn counts how many recent commits touched each path. It reads history only: a
// session must leave the repository exactly as it found it.
func TestChurnCountsCommitsPerPath(t *testing.T) {
	dir := newRepo(t, map[string]string{
		"a.go": "package a\n",
		"b.go": "package a\n",
	})

	// The initial commit touched both files; then a.go twice and b.go once.
	writeFile(t, dir, "a.go", "package a\n\n// one\n")
	gitIn(t, dir, "commit", "-am", "one")
	writeFile(t, dir, "a.go", "package a\n\n// one\n// two\n")
	gitIn(t, dir, "commit", "-am", "two")
	writeFile(t, dir, "b.go", "package a\n\n// b\n")
	gitIn(t, dir, "commit", "-am", "three")

	before := snapshotTree(t, dir)
	r := openRepo(t, dir)
	churn, err := r.Churn()
	if err != nil {
		t.Fatal(err)
	}
	if churn["a.go"] != 3 {
		t.Errorf("a.go churn = %d, want 3 (initial plus two)", churn["a.go"])
	}
	if churn["b.go"] != 2 {
		t.Errorf("b.go churn = %d, want 2 (initial plus one)", churn["b.go"])
	}
	if churn["missing.go"] != 0 {
		t.Errorf("a path that never changed has churn %d", churn["missing.go"])
	}
	if got := snapshotTree(t, dir); !reflect.DeepEqual(got, before) {
		t.Error("reading churn changed the repository")
	}
}
