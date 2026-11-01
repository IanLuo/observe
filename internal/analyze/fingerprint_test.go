package analyze

import (
	"strings"
	"testing"
)

func fixtureInput() FingerprintInput {
	return FingerprintInput{
		RepoIdentity:  "/repos/.git",
		Commit:        "abc123",
		DiffHash:      "diff-hash",
		Specs:         []File{{Path: ".agents/specs/spec.md", Body: []byte("locked goal")}},
		Sources:       []File{{Path: "main.go", Body: []byte("package main")}},
		SchemaVersion: "cache/1",
	}
}

// The fingerprint must be stable: two analyses of the same inputs cannot look
// like different inputs.
func TestFingerprintIsDeterministic(t *testing.T) {
	first := fixtureInput()
	first.Specs = append(first.Specs, File{Path: "z.md", Body: []byte("z")})
	first.Sources = append(first.Sources, File{Path: "a.go", Body: []byte("package a")})

	second := fixtureInput()
	second.Specs = append([]File{{Path: "z.md", Body: []byte("z")}}, second.Specs...)
	second.Sources = append([]File{{Path: "a.go", Body: []byte("package a")}}, second.Sources...)

	if Fingerprint(first).Key() != Fingerprint(second).Key() {
		t.Fatalf("input order changed the fingerprint:\n%s\n%s", Fingerprint(first).Key(), Fingerprint(second).Key())
	}
}

// The fingerprint carries a hash of every input and no input text, so a cached
// record cannot leak source or spec content (AC12).
func TestFingerprintHashesContentInsteadOfCarryingIt(t *testing.T) {
	in := fixtureInput()
	in.Specs[0].Body = []byte("the distinctive locked sentence")
	got := Fingerprint(in)

	if strings.Contains(got.Key(), "distinctive") {
		t.Fatalf("spec text leaked into the fingerprint: %s", got.Key())
	}
	if got.SpecHashes[".agents/specs/spec.md"] == "" {
		t.Fatal("spec content hash not recorded")
	}
	if got.AnalyzerVersion == "" {
		t.Error("analyzer version not recorded")
	}
	if got.SchemaVersion != "cache/1" {
		t.Errorf("schema version = %q, want the caller's", got.SchemaVersion)
	}
}

// Any input change makes the cache stale, which is what AC9 rests on.
func TestFingerprintChangesWithEveryInput(t *testing.T) {
	base := Fingerprint(fixtureInput()).Key()

	mutations := map[string]func(*FingerprintInput){
		"repository identity": func(in *FingerprintInput) { in.RepoIdentity = "/other/.git" },
		"commit":              func(in *FingerprintInput) { in.Commit = "def456" },
		"selected diff":       func(in *FingerprintInput) { in.DiffHash = "other-diff" },
		"spec content":        func(in *FingerprintInput) { in.Specs[0].Body = []byte("changed goal") },
		"spec set":            func(in *FingerprintInput) { in.Specs = nil },
		"source content":      func(in *FingerprintInput) { in.Sources[0].Body = []byte("package other") },
		"source set":          func(in *FingerprintInput) { in.Sources = nil },
		"cache schema":        func(in *FingerprintInput) { in.SchemaVersion = "cache/2" },
	}

	for name, mutate := range mutations {
		in := fixtureInput()
		mutate(&in)
		if got := Fingerprint(in).Key(); got == base {
			t.Errorf("%s changed but the fingerprint did not", name)
		}
	}
}
