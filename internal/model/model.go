// Package model holds the shared snapshot and status contract.
//
// It imports no other internal package: every package depends on model, never
// the reverse (.agents/specs/architecture.md, Layers & ownership). The
// vocabularies below are fixed by .agents/specs/observe-progress-view.md; adding
// a value is a product decision, so nothing here may be extended by a caller
// inventing its own string.
package model

// Relation is how code relates to a goal. An unmapped change is never rendered
// as aligned, and no value may be upgraded during presentation.
type Relation string

const (
	RelationAppearsToSupport Relation = "appears-to-support"
	RelationConflict         Relation = "conflict"
	RelationUnmapped         Relation = "unmapped"
	RelationUnavailable      Relation = "unavailable"
	RelationUnknown          Relation = "unknown"
	RelationStale            Relation = "stale"
)

// Verification is a separate axis from Relation. Code presence alone never
// yields VerificationVerified: only explicit evidence does (AC3).
type Verification string

const (
	VerificationVerified Verification = "verified"
	VerificationUnknown  Verification = "unknown"
	VerificationFailed   Verification = "failed"
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

// GoalAssessment is the per-goal result: a relation on one axis and a
// verification on another.
type GoalAssessment struct {
	Locator              GoalLocator
	Relation             Relation
	Inferred             bool
	Evidence             []EvidenceRef
	Verification         Verification
	VerificationEvidence []EvidenceRef
	Diagnostics          []Diagnostic
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
