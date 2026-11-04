package analyze

import (
	"crypto/sha256"
	"encoding/hex"

	"observe/internal/model"
)

// AnalyzerVersion changes whenever a rule in this package changes what it emits.
// It is part of the input fingerprint, so an upgrade invalidates a cache record
// written by an older rule set instead of presenting old statuses as current.
const AnalyzerVersion = "observe-analyzer/3"

// FingerprintInput is everything an analysis depends on. It has no prompt field
// on purpose: a task prompt is transient context, never an input that could
// invalidate or enter a persisted record (AC12).
type FingerprintInput struct {
	RepoIdentity  string
	Commit        string
	DiffHash      string
	Specs         []File // locked specs, as read
	Sources       []File // tracked source in the V1 language set, as read
	SchemaVersion string // owned by the cache, which refuses records it cannot read
}

// Fingerprint records what an analysis was computed from, as hashes. It is
// deterministic and carries no file text.
func Fingerprint(in FingerprintInput) model.InputFingerprint {
	return model.InputFingerprint{
		RepoIdentity:    in.RepoIdentity,
		Commit:          in.Commit,
		DiffHash:        in.DiffHash,
		SpecHashes:      hashes(in.Specs),
		SourceHashes:    hashes(in.Sources),
		AnalyzerVersion: AnalyzerVersion,
		SchemaVersion:   in.SchemaVersion,
	}
}

// hashes maps each path to a content hash. A path that appears twice keeps one
// hash, because the spec and the source sets are keyed by path.
func hashes(files []File) map[string]string {
	out := make(map[string]string, len(files))
	for _, f := range files {
		sum := sha256.Sum256(f.Body)
		out[f.Path] = hex.EncodeToString(sum[:])
	}
	return out
}
