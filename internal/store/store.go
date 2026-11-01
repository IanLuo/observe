// Package store is the disposable current-analysis cache described in
// .agents/specs/system-design.md (Components, Evidence store).
//
// One SQLite file outside the repository holds at most one record per repository
// identity: the identity is the primary key, so a completed analysis replaces the
// previous one and no history can accumulate. The record holds derived statuses
// and evidence references only — never source text, never the task prompt
// (AC12). An unreadable, stale, or future-schema record is discarded and
// reported, never partially loaded.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"observe/internal/model"
)

// SchemaVersion is the cache record format. A record carrying another value is
// discarded instead of guessed, because a wrong guess could present old statuses
// as current (system-design, Cache compatibility).
const SchemaVersion = "observe-cache/1"

// schemaVersion is the on-disk layout number. A file written by a newer build is
// refused rather than migrated by guesswork.
const schemaVersion = 1

// Cache is one open cache file.
type Cache struct {
	db   *sql.DB
	path string
}

// LoadResult is one cache read. Fresh means the record matched the fingerprint it
// was checked against; otherwise the result is an absence and Diagnostics says
// why the record is gone.
type LoadResult struct {
	Snapshot    model.Snapshot
	Fresh       bool
	Diagnostics []model.Diagnostic
}

// DefaultPath is where the cache lives when the caller has no preference: the
// user cache directory, outside every repository.
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate the user cache directory: %w", err)
	}
	return filepath.Join(dir, "observe", "analysis.db"), nil
}

// Open opens or creates the cache at path.
//
// It refuses a path inside repoRoot, so a fallback can never be written into the
// analyzed repository, and it keeps the file and its directory readable by the
// user alone where the filesystem supports that.
func Open(path, repoRoot string) (*Cache, error) {
	if path == "" {
		return nil, errors.New("cache path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve cache path %q: %w", path, err)
	}
	if insideRoot(abs, repoRoot) {
		return nil, fmt.Errorf("refuse cache path inside the repository: %s", abs)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}

	db, err := sql.Open("sqlite3", abs)
	if err != nil {
		return nil, fmt.Errorf("open cache %s: %w", abs, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open cache %s: %w", abs, err)
	}
	if err := prepare(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("restrict cache permissions: %w", err)
	}
	return &Cache{db: db, path: abs}, nil
}

// Path is the absolute path of the open cache file.
func (c *Cache) Path() string { return c.path }

// Close releases the cache file.
func (c *Cache) Close() error { return c.db.Close() }

// prepare creates the schema, or refuses a file from a newer layout.
func prepare(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read cache layout version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("cache layout %d is newer than this build's %d; clear the cache", version, schemaVersion)
	}

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS analysis (
			repo_identity    TEXT PRIMARY KEY,
			fingerprint      TEXT NOT NULL,
			schema_version   TEXT NOT NULL,
			analyzer_version TEXT NOT NULL,
			saved_at         INTEGER NOT NULL,
			record           TEXT NOT NULL
		)`)
	if err != nil {
		return fmt.Errorf("create cache schema: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return fmt.Errorf("record cache layout version: %w", err)
	}
	return nil
}

// persisted is the stored part of a snapshot. The fingerprint is not stored
// twice: it is its own column, and a loaded snapshot is labelled with the
// fingerprint it was checked against, so a caller never receives an old one.
type persisted struct {
	Generation  string
	CreatedAt   time.Time
	Freshness   model.Freshness
	Assessments []model.GoalAssessment
	Diagnostics []model.Diagnostic
}

// Save writes the snapshot as the current record for its repository identity,
// replacing any earlier record.
//
// It refuses a record it could not read back: no identity, or a schema version
// this build does not understand. Refusing here keeps "written" and "readable" the
// same set.
func (c *Cache) Save(s model.Snapshot) error {
	if s.Fingerprint.RepoIdentity == "" {
		return errors.New("refuse a cache record with no repository identity")
	}
	if s.Fingerprint.SchemaVersion != SchemaVersion {
		return fmt.Errorf("refuse a cache record for schema %q; this build writes %q", s.Fingerprint.SchemaVersion, SchemaVersion)
	}

	fingerprint, err := json.Marshal(s.Fingerprint)
	if err != nil {
		return fmt.Errorf("encode fingerprint: %w", err)
	}
	record, err := json.Marshal(persisted{
		Generation:  s.Generation,
		CreatedAt:   s.CreatedAt,
		Freshness:   s.Freshness,
		Assessments: s.Assessments,
		Diagnostics: s.Diagnostics,
	})
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}

	_, err = c.db.Exec(`
		INSERT INTO analysis (repo_identity, fingerprint, schema_version, analyzer_version, saved_at, record)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_identity) DO UPDATE SET
			fingerprint      = excluded.fingerprint,
			schema_version   = excluded.schema_version,
			analyzer_version = excluded.analyzer_version,
			saved_at         = excluded.saved_at,
			record           = excluded.record`,
		s.Fingerprint.RepoIdentity,
		string(fingerprint),
		s.Fingerprint.SchemaVersion,
		s.Fingerprint.AnalyzerVersion,
		time.Now().UTC().Unix(),
		string(record),
	)
	if err != nil {
		return fmt.Errorf("write cache record: %w", err)
	}
	return nil
}

// Load returns the current record for the repository identity in current.
//
// A record is fresh only when its whole fingerprint matches. Otherwise it is
// discarded — the inputs changed, the schema is not this build's, or the bytes are
// unreadable — and the reason is reported. No prior record is an absence, not an
// error: a first run has nothing to restore and must not look like a failure.
func (c *Cache) Load(current model.InputFingerprint) (LoadResult, error) {
	if current.RepoIdentity == "" {
		return LoadResult{}, errors.New("refuse a cache read with no repository identity")
	}

	var storedFingerprint, schema, record string
	err := c.db.QueryRow(
		`SELECT fingerprint, schema_version, record FROM analysis WHERE repo_identity = ?`,
		current.RepoIdentity,
	).Scan(&storedFingerprint, &schema, &record)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return LoadResult{}, nil
	case err != nil:
		return LoadResult{}, fmt.Errorf("read cache record: %w", err)
	}

	if schema != SchemaVersion {
		return c.discard(current.RepoIdentity, fmt.Sprintf(
			"discarded a cache record for schema %q; this build reads %q, so it is recomputed", schema, SchemaVersion))
	}

	var previous model.InputFingerprint
	if err := json.Unmarshal([]byte(storedFingerprint), &previous); err != nil {
		return c.discard(current.RepoIdentity, "discarded an unreadable cache fingerprint: "+err.Error())
	}
	if previous.Key() != current.Key() {
		return c.discard(current.RepoIdentity, "discarded a cache record whose inputs changed: "+
			strings.Join(previous.Differences(current), ", "))
	}

	var stored persisted
	if err := json.Unmarshal([]byte(record), &stored); err != nil {
		return c.discard(current.RepoIdentity, "discarded an unreadable cache record: "+err.Error())
	}

	return LoadResult{
		Fresh: true,
		Snapshot: model.Snapshot{
			Fingerprint: current,
			Generation:  stored.Generation,
			CreatedAt:   stored.CreatedAt,
			Freshness:   stored.Freshness,
			Assessments: stored.Assessments,
			Diagnostics: stored.Diagnostics,
		},
	}, nil
}

// discard removes the record and reports why, so the caller can recompute.
func (c *Cache) discard(identity, message string) (LoadResult, error) {
	if err := c.Clear(); err != nil {
		return LoadResult{}, err
	}
	return LoadResult{Diagnostics: []model.Diagnostic{{Op: "cache", Message: message}}}, nil
}

// Clear removes every record. The cache is disposable, so this needs no
// confirmation and leaves no history behind.
func (c *Cache) Clear() error {
	if _, err := c.db.Exec(`DELETE FROM analysis`); err != nil {
		return fmt.Errorf("clear cache: %w", err)
	}
	return nil
}

// insideRoot reports whether path is the repository root or below it. Both sides
// are absolute and cleaned, so a lexical check is enough to keep a fallback out of
// the repository.
func insideRoot(path, root string) bool {
	if root == "" {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, path)
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
