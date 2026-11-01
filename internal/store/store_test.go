package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"observe/internal/model"
)

func fixtureFingerprint() model.InputFingerprint {
	return model.InputFingerprint{
		RepoIdentity:    "/repos/one/.git",
		Commit:          "abc123",
		DiffHash:        "diff-hash",
		SpecHashes:      map[string]string{".agents/specs/spec.md": "spec-hash"},
		SourceHashes:    map[string]string{"main.go": "source-hash"},
		AnalyzerVersion: "test-analyzer",
		SchemaVersion:   SchemaVersion,
	}
}

func fixtureSnapshot(t *testing.T, fingerprint model.InputFingerprint) model.Snapshot {
	t.Helper()
	return model.Snapshot{
		Fingerprint: fingerprint,
		Generation:  "gen-1",
		CreatedAt:   time.Unix(1_700_000_000, 0).UTC(),
		Freshness:   model.FreshnessCurrent,
		Assessments: []model.GoalAssessment{{
			Locator: model.GoalLocator{
				SpecPath:     ".agents/specs/spec.md",
				HeadingChain: []string{"Acceptance criteria"},
				TextHash:     "goal-hash",
				Label:        "the reader exposes `Open`",
			},
			Relation:     model.RelationAppearsToSupport,
			Inferred:     true,
			Verification: model.VerificationUnknown,
			Evidence: []model.EvidenceRef{{
				Path: "internal/repo/repo.go", Symbol: "Open", LineStart: 3, LineEnd: 5,
				Kind: model.EvidenceDeclaration, Provenance: model.ProvenanceParserRule, Confidence: 0.9,
			}},
		}},
	}
}

// openTestCache opens a cache in its own temporary directory, which stands in for
// "outside the repository".
func openTestCache(t *testing.T) *Cache {
	t.Helper()
	root := t.TempDir()
	c, err := Open(filepath.Join(root, "cache", "analysis.db"), filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// AC12: one current record, which a second analysis replaces rather than appends
// to.
func TestSaveReplacesTheCurrentRecordInsteadOfAppendingHistory(t *testing.T) {
	c := openTestCache(t)

	fp := fixtureFingerprint()
	if err := c.Save(fixtureSnapshot(t, fp)); err != nil {
		t.Fatal(err)
	}

	next := fixtureFingerprint()
	next.Commit = "def456"
	snapshot := fixtureSnapshot(t, next)
	snapshot.Generation = "gen-2"
	if err := c.Save(snapshot); err != nil {
		t.Fatal(err)
	}

	got, err := c.Load(next)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Fresh {
		t.Fatalf("the second snapshot is not current: %+v", got)
	}
	if got.Snapshot.Generation != "gen-2" {
		t.Errorf("generation = %q, want gen-2", got.Snapshot.Generation)
	}
	if rows := countRows(t, c); rows != 1 {
		t.Fatalf("%d cache rows, want exactly 1: the cache keeps no history", rows)
	}
}

// AC12: the cache is replaceable, and a cleared cache is simply absent.
func TestClearRemovesTheCurrentRecord(t *testing.T) {
	c := openTestCache(t)
	fp := fixtureFingerprint()
	if err := c.Save(fixtureSnapshot(t, fp)); err != nil {
		t.Fatal(err)
	}

	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	got, err := c.Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fresh || got.Snapshot.Generation != "" {
		t.Fatalf("cleared cache still holds a record: %+v", got)
	}
}

// AC12: the record holds derived statuses and references, never source text.
func TestCacheStoresNoSourceText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "analysis.db")
	c, err := Open(path, filepath.Join(dir, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	snapshot := fixtureSnapshot(t, fixtureFingerprint())
	if err := c.Save(snapshot); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"package repo", "func Open()", "distinctive source line"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("the cache file contains source text: %q", leaked)
		}
	}
	// The reference itself must be there, or the check above would pass on an
	// empty file.
	if !strings.Contains(string(raw), "internal/repo/repo.go") {
		t.Error("the cache does not hold the evidence reference it was given")
	}
}

// AC9, cache half: inputs changed, so the record is stale — it is discarded and
// reported, never presented as current.
func TestLoadDiscardsARecordWhoseInputsChanged(t *testing.T) {
	c := openTestCache(t)
	saved := fixtureFingerprint()
	if err := c.Save(fixtureSnapshot(t, saved)); err != nil {
		t.Fatal(err)
	}

	moved := saved
	moved.Commit = "def456"
	got, err := c.Load(moved)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fresh {
		t.Fatal("a record from different inputs was reported as current")
	}
	if len(got.Diagnostics) == 0 || !strings.Contains(got.Diagnostics[0].Message, "commit") {
		t.Fatalf("the discard does not name the changed input: %+v", got.Diagnostics)
	}
	if rows := countRows(t, c); rows != 0 {
		t.Fatalf("%d stale rows kept; a stale record must not survive as history", rows)
	}
}

// A record written by an unknown future schema is not guessed or partially
// loaded (system-design, Cache compatibility).
func TestLoadRefusesARecordFromAnUnknownSchema(t *testing.T) {
	c := openTestCache(t)
	fp := fixtureFingerprint()
	if err := c.Save(fixtureSnapshot(t, fp)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`UPDATE analysis SET schema_version = 'observe-cache/99'`); err != nil {
		t.Fatal(err)
	}

	got, err := c.Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fresh || len(got.Diagnostics) == 0 {
		t.Fatalf("unknown schema loaded without a diagnostic: %+v", got)
	}
	if rows := countRows(t, c); rows != 0 {
		t.Fatalf("%d rows from an unknown schema kept", rows)
	}
}

// AC9: a failed refresh must not destroy the last successful record, and a fresh
// session with no prior success reports absence rather than an empty success.
func TestFailedRefreshLeavesTheCurrentRecordAndAbsenceIsNotAnError(t *testing.T) {
	c := openTestCache(t)
	fp := fixtureFingerprint()

	absent, err := c.Load(fp)
	if err != nil {
		t.Fatalf("an empty cache is not an error: %v", err)
	}
	if absent.Fresh || len(absent.Diagnostics) != 0 {
		t.Fatalf("empty cache = %+v, want a plain absence", absent)
	}

	if err := c.Save(fixtureSnapshot(t, fp)); err != nil {
		t.Fatal(err)
	}
	// The refresh fails here: nothing is saved.
	got, err := c.Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Fresh || got.Snapshot.Generation != "gen-1" {
		t.Fatalf("the last successful record was lost: %+v", got)
	}
}

// AC12 and the architecture: the cache lives outside the repository, and never
// writes a fallback inside it.
func TestOpenRefusesAPathInsideTheRepository(t *testing.T) {
	root := t.TempDir()
	if _, err := Open(filepath.Join(root, "analysis.db"), root); err == nil {
		t.Fatal("Open accepted a cache path inside the repository")
	}
}

// AC12: the cache is user-only where the filesystem supports it.
func TestCacheFileIsUserOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "analysis.db")
	c, err := Open(path, filepath.Join(dir, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Save(fixtureSnapshot(t, fixtureFingerprint())); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("cache file mode %o is reachable by other users", perm)
	}
}

// Save refuses a snapshot that a later read would have to reject.
func TestSaveRefusesARecordItCouldNotRead(t *testing.T) {
	c := openTestCache(t)

	noIdentity := fixtureSnapshot(t, fixtureFingerprint())
	noIdentity.Fingerprint.RepoIdentity = ""
	if err := c.Save(noIdentity); err == nil {
		t.Error("Save accepted a record with no repository identity")
	}

	wrongSchema := fixtureSnapshot(t, fixtureFingerprint())
	wrongSchema.Fingerprint.SchemaVersion = "observe-cache/99"
	if err := c.Save(wrongSchema); err == nil {
		t.Error("Save accepted a record from an unknown schema")
	}
	if rows := countRows(t, c); rows != 0 {
		t.Fatalf("%d rows written by a refused save", rows)
	}
}

func countRows(t *testing.T, c *Cache) int {
	t.Helper()
	var rows int
	if err := c.db.QueryRow(`SELECT count(*) FROM analysis`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}
