// Package model holds the shared snapshot and status contract.
//
// It imports no other internal package: every package depends on model, never
// the reverse (.agents/specs/architecture.md, Layers & ownership). The
// vocabularies below are fixed by .agents/specs/observe-progress-view.md; adding
// a value is a product decision, so nothing here may be extended by a caller
// inventing its own string.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// CheckStatus is the verdict of one goal check. Pass and violated come from an
// assertion evaluated against the repository; unknown means the assertion could
// not be decided, and it always carries a reason. No verdict is ever upgraded
// during presentation.
type CheckStatus string

const (
	CheckPass     CheckStatus = "pass"
	CheckViolated CheckStatus = "violated"
	CheckUnknown  CheckStatus = "unknown"
)

// Verification is a separate axis from Relation. Code presence alone never
// yields VerificationVerified: only explicit evidence does (AC3).
type Verification string

const (
	VerificationVerified Verification = "verified"
	VerificationUnknown  Verification = "unknown"
)

// Freshness is the state of a whole snapshot relative to its inputs.
type Freshness string

const (
	FreshnessCurrent Freshness = "current"
	FreshnessStale   Freshness = "stale"
	FreshnessFailed  Freshness = "failed"
)

// BaselineState says whether goal alignment has a usable goal baseline at all.
type BaselineState string

const (
	BaselineAvailable   BaselineState = "available"
	BaselineMissing     BaselineState = "missing"     // no usable locked spec (AC7)
	BaselineConflicting BaselineState = "conflicting" // two locked specs, neither preferred (AC8)
)

// EvidenceKind is what a reference points at.
type EvidenceKind string

const (
	EvidenceDeclaration EvidenceKind = "declaration"
	EvidenceReference   EvidenceKind = "reference"
	EvidenceTest        EvidenceKind = "test"
	EvidenceSpec        EvidenceKind = "spec"
	EvidenceOther       EvidenceKind = "other"
)

// Provenance is how a reference was found. Manual context is never proof.
type Provenance string

const (
	ProvenanceExactMatch    Provenance = "exact-match"
	ProvenanceParserRule    Provenance = "parser-rule"
	ProvenanceManualContext Provenance = "manual-context"
)

// GoalLocator identifies one requirement inside a locked spec. It is derived, so
// it can be rebuilt from the spec; the full spec text is never copied into the
// cache (.agents/specs/system-design.md, Data model).
type GoalLocator struct {
	SpecPath     string   // repository-relative
	HeadingChain []string // enclosing headings, outermost first
	TextHash     string   // normalized requirement text, hex sha256
	Label        string   // display label only; may be empty
}

// EvidenceRef is one provenance-bearing pointer into the repository. It is not
// proof of runtime behaviour.
type EvidenceRef struct {
	Path       string
	Symbol     string
	LineStart  int
	LineEnd    int
	Kind       EvidenceKind
	Provenance Provenance
	Confidence float64 // 0..1, from the producing rule
	Verifier   string
}

// Diagnostic reports something the analyzer could not justify. An empty success
// is never substituted for a diagnostic.
type Diagnostic struct {
	Path    string
	Op      string
	Message string
}

// Check is one assertion declared in a locked spec and evaluated against the
// repository. Touched says the selected diff changed a file the check's evidence
// depends on; it never makes a check pass (AC2).
type Check struct {
	// Measures is what the check looked at. A passing presence check proves a name
	// exists, not that behaviour holds, so the reader is told which one it was
	// (AC15).
	Measures  Measures
	ID        string
	Subject   string // "symbol Store", "module internal/analyze", "file web/main.ts"
	Assertion string // "exists", "absent", "imports internal/repo", "has-test"
	Status    CheckStatus
	Reason    string // why the verdict is unknown; empty for pass and violated
	Evidence  []EvidenceRef
	// Inspected is what a verdict rests on when there is nothing to point at: a
	// passing "absent" or "not-imports" check inspects a set and finds nothing.
	Inspected int
	// Verification is evidence that a test exercises this check. It is a separate
	// axis: a verified check may still be violated, and a test that names a check
	// proves coverage, never a passing result (AC3).
	Verification         Verification
	VerificationEvidence []EvidenceRef
	Touched              bool
	SpecPath             string
	Line                 int // where the check is declared, 1-based
}

// Measures names what a check looked at, so a verdict is read at its real strength.
type Measures string

const (
	// MeasuresPresence: a symbol, module, or file does or does not exist.
	MeasuresPresence Measures = "presence"
	// MeasuresDependency: one module does or does not import another.
	MeasuresDependency Measures = "dependency"
	// MeasuresTest: a module carries test declarations.
	MeasuresTest Measures = "test"
)

// GoalAssessment is the per-goal result: the checks the spec declares for it, on
// one axis, and a verification on another.
type GoalAssessment struct {
	Locator GoalLocator
	// Invariant marks a bullet from a locked document that is not the goal spec:
	// it carries checks, and it is never counted as a goal (AC14).
	Invariant            bool
	Checks               []Check
	Verification         Verification
	VerificationEvidence []EvidenceRef
	Diagnostics          []Diagnostic
}

// PromptConflict is a task-prompt statement that contradicts a locked goal. The
// locked spec stays the baseline and the conflict is left unresolved: the prompt
// is supporting context and never overrides a spec (AC5).
type PromptConflict struct {
	Locator    GoalLocator
	Prompt     string // the contradicting prompt sentence, quoted
	Diagnostic string // what the rule compared, for the user to judge
}

// SpecBaseline is the goal baseline read from the repository's locked specs.
type SpecBaseline struct {
	State       BaselineState
	SpecPaths   []string // the docs that would serve as the baseline, sorted
	Diagnostics []Diagnostic
}

// InputFingerprint is everything that, if changed, makes an analysis stale.
// The selected diff is carried as a hash only; prompt text is never persisted.
type InputFingerprint struct {
	RepoIdentity    string
	Commit          string
	DiffHash        string
	SpecHashes      map[string]string // spec path -> content hash
	SourceHashes    map[string]string // source path -> content hash
	AnalyzerVersion string
	SchemaVersion   string
}

// Key is a canonical form of the fingerprint: two fingerprints have the same key
// exactly when every input matches. Maps are sorted, so the key never depends on
// map iteration order.
func (f InputFingerprint) Key() string {
	var b strings.Builder
	write := func(name, value string) {
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(value)
		b.WriteByte('\n')
	}

	write("repo", f.RepoIdentity)
	write("commit", f.Commit)
	write("diff", f.DiffHash)
	for _, path := range sortedPaths(f.SpecHashes) {
		write("spec:"+path, f.SpecHashes[path])
	}
	for _, path := range sortedPaths(f.SourceHashes) {
		write("source:"+path, f.SourceHashes[path])
	}
	write("analyzer", f.AnalyzerVersion)
	write("schema", f.SchemaVersion)
	return b.String()
}

// Differences names the inputs that differ between two fingerprints, so a
// discarded cache can say what changed instead of only that something did.
func (f InputFingerprint) Differences(g InputFingerprint) []string {
	var changed []string
	note := func(name string, differs bool) {
		if differs {
			changed = append(changed, name)
		}
	}

	note("repository identity", f.RepoIdentity != g.RepoIdentity)
	note("commit", f.Commit != g.Commit)
	note("selected diff", f.DiffHash != g.DiffHash)
	note("analyzer version", f.AnalyzerVersion != g.AnalyzerVersion)
	note("cache schema version", f.SchemaVersion != g.SchemaVersion)

	changed = append(changed, hashDifferences("spec", f.SpecHashes, g.SpecHashes)...)
	changed = append(changed, hashDifferences("source", f.SourceHashes, g.SourceHashes)...)
	sort.Strings(changed)
	return changed
}

// hashDifferences names the paths whose hash was added, removed, or changed.
func hashDifferences(kind string, from, to map[string]string) []string {
	seen := make(map[string]bool, len(from)+len(to))
	var changed []string
	for _, path := range sortedPaths(from) {
		seen[path] = true
		if to[path] != from[path] {
			changed = append(changed, kind+":"+path)
		}
	}
	for _, path := range sortedPaths(to) {
		if !seen[path] {
			changed = append(changed, kind+":"+path)
		}
	}
	return changed
}

func sortedPaths(hashes map[string]string) []string {
	paths := make([]string, 0, len(hashes))
	for path := range hashes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// Generation is a short, stable identifier for one analysis of exactly these
// inputs. Two runs over unchanged inputs share it; any input change gives a new
// one.
func (f InputFingerprint) Generation() string {
	sum := sha256.Sum256([]byte(f.Key()))
	return hex.EncodeToString(sum[:])[:12]
}

// Snapshot is one immutable analysis result, and the only record the cache
// persists (.agents/specs/system-design.md, Data model). It carries derived
// statuses and references, never source text and never the task prompt: prompt
// conflicts stay in memory, because the prompt is transient context and is not
// persisted.
type Snapshot struct {
	Fingerprint InputFingerprint
	Generation  string
	CreatedAt   time.Time
	Freshness   Freshness
	Assessments []GoalAssessment
	Diagnostics []Diagnostic
}
