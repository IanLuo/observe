package model

import "testing"

// A generation identifies one analysis of exactly one input set, so an unchanged
// repository reuses it and any change replaces it.
func TestGenerationIsStableForTheSameInputsAndChangesWithThem(t *testing.T) {
	base := InputFingerprint{RepoIdentity: "/repos/one/.git", Commit: "abc123", SchemaVersion: "cache/1"}
	again := base

	if base.Generation() != again.Generation() {
		t.Fatal("the same inputs produced two generations")
	}

	changed := base
	changed.Commit = "def456"
	if changed.Generation() == base.Generation() {
		t.Fatal("a changed input kept the same generation")
	}
	if len(base.Generation()) != 12 {
		t.Errorf("generation %q is not a short identifier", base.Generation())
	}
}

// Differences names what changed, for the diagnostic a discarded cache reports.
func TestDifferencesNamesChangedInputs(t *testing.T) {
	before := InputFingerprint{
		RepoIdentity: "/repos/one/.git", Commit: "abc123", DiffHash: "d1",
		SpecHashes: map[string]string{"spec.md": "s1"}, SourceHashes: map[string]string{"main.go": "h1"},
		AnalyzerVersion: "a/1", SchemaVersion: "c/1",
	}
	after := before
	after.Commit = "def456"
	after.SourceHashes = map[string]string{"main.go": "h2", "new.go": "h3"}

	got := before.Differences(after)
	want := map[string]bool{"commit": true, "source:main.go": true, "source:new.go": true}
	if len(got) != len(want) {
		t.Fatalf("differences = %v, want %v", got, want)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected difference %q", name)
		}
	}
}
