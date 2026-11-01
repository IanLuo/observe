package serve

import (
	"time"

	"observe/internal/analyze"
	"observe/internal/model"
	"observe/internal/repo"
)

// State is the whole presented view: the latest snapshot, its freshness, and what
// the analysis is doing (.agents/specs/design-system.md owns the visual tokens;
// this type owns the wire contract the UI renders from).
//
// Every level of the AC4 drill-down is in one payload: a goal carries its spec
// locator and evidence references, and each reference carries a path, a symbol,
// and a line range, so the UI can go goal → evidence → exact source line without
// another request.
type State struct {
	Repository  string     `json:"repository"`
	Generation  string     `json:"generation,omitempty"`
	Freshness   string     `json:"freshness"` // updating, current, stale, failed
	UpdatedAt   string     `json:"updated_at,omitempty"`
	Failure     *Failure   `json:"failure,omitempty"`
	PromptShown bool       `json:"prompt_shown"`
	Baseline    Baseline   `json:"baseline"`
	Diff        Diff       `json:"diff"`
	Goals       []Goal     `json:"goals"`
	Conflicts   []Conflict `json:"prompt_conflicts"`
	Diagnostics []string   `json:"diagnostics"`
	Cache       Cache      `json:"cache"`
}

// Failure is the last refresh failure, which the view keeps visible even while
// stale data is on screen (AC9).
type Failure struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

// Baseline reports whether goal alignment has a usable goal baseline.
type Baseline struct {
	State string   `json:"state"` // available, missing, conflicting
	Specs []string `json:"specs"`
}

// Diff is the selected diff, summarised per path.
type Diff struct {
	Hash  string     `json:"hash"`
	Files []DiffFile `json:"files"`
}

// DiffFile is one changed path. Lines is bounded so the page stays readable;
// Truncated says how many changed lines were left out.
type DiffFile struct {
	Path      string     `json:"path"`
	Status    string     `json:"status"`
	Added     int        `json:"added"`
	Removed   int        `json:"removed"`
	Lines     []DiffLine `json:"lines"`
	Truncated int        `json:"truncated"`
}

// DiffLine is one added or removed line, numbered on its own side.
type DiffLine struct {
	Kind string `json:"kind"` // added, removed
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Goal is one requirement with its relation to the selected diff and the evidence
// that justifies it. Relation and Verification are separate axes: no value here
// can add up to "verified" without explicit verification evidence (AC3).
type Goal struct {
	SpecPath             string   `json:"spec_path"`
	Heading              []string `json:"heading"`
	Label                string   `json:"label"`
	TextHash             string   `json:"text_hash"`
	Relation             string   `json:"relation"`
	Inferred             bool     `json:"inferred"`
	Verification         string   `json:"verification"`
	Evidence             []Ref    `json:"evidence"`
	VerificationEvidence []Ref    `json:"verification_evidence"`
	Diagnostics          []string `json:"diagnostics"`
}

// Ref is one evidence reference: a repository path, a symbol, and a line range.
type Ref struct {
	Path       string  `json:"path"`
	Symbol     string  `json:"symbol"`
	LineStart  int     `json:"line_start"`
	LineEnd    int     `json:"line_end"`
	Kind       string  `json:"kind"`
	Provenance string  `json:"provenance"`
	Confidence float64 `json:"confidence"`
	Verifier   string  `json:"verifier,omitempty"`
}

// Conflict is a task-prompt sentence that contradicts a locked goal. It is shown
// and never resolved: the prompt is context, the spec stays the baseline (AC5).
type Conflict struct {
	SpecPath string `json:"spec_path"`
	Label    string `json:"label"`
	Prompt   string `json:"prompt"`
	Reason   string `json:"reason"`
}

// Cache describes the disposable current-state cache.
type Cache struct {
	Path  string `json:"path"`
	State string `json:"state"` // disabled, absent, current, saving, saved
}

// maxDiffLines is how many changed lines per path the view carries.
const maxDiffLines = 40

// newState maps one analysis pass to the wire contract. stale marks every
// relation stale: after a failed refresh the evidence stays readable, but no
// status is presented as current (AC9).
func newState(a Analyzed, freshness string, failure *Failure, cache Cache) State {
	state := State{
		Repository:  a.Repository,
		Generation:  a.Snapshot.Generation,
		Freshness:   freshness,
		PromptShown: a.PromptShown,
		Baseline: Baseline{
			State: string(a.Baseline.State),
			Specs: append([]string{}, a.Baseline.SpecPaths...),
		},
		Diff: Diff{
			Hash:  a.Snapshot.Fingerprint.DiffHash,
			Files: diffFiles(a.Changes),
		},
		Goals:       goals(a.Snapshot.Assessments, freshness),
		Diagnostics: messages(a.Snapshot.Diagnostics),
		Cache:       cache,
	}
	if !a.Snapshot.CreatedAt.IsZero() {
		state.UpdatedAt = a.Snapshot.CreatedAt.Format(time.RFC3339)
	}
	state.Failure = failure
	for _, d := range a.Baseline.Diagnostics {
		state.Diagnostics = append(state.Diagnostics, d.Message)
	}
	for _, c := range a.Conflicts {
		state.Conflicts = append(state.Conflicts, Conflict{
			SpecPath: c.Locator.SpecPath,
			Label:    c.Locator.Label,
			Prompt:   c.Prompt,
			Reason:   c.Diagnostic,
		})
	}
	return state
}

// goals maps assessments, downgrading every relation to stale when the snapshot
// is no longer current.
func goals(assessments []model.GoalAssessment, freshness string) []Goal {
	out := make([]Goal, len(assessments))
	for i, a := range assessments {
		relation := string(a.Relation)
		if freshness == string(model.FreshnessStale) || freshness == string(model.FreshnessFailed) {
			relation = string(model.RelationStale)
		}
		out[i] = Goal{
			SpecPath:             a.Locator.SpecPath,
			Heading:              append([]string{}, a.Locator.HeadingChain...),
			Label:                a.Locator.Label,
			TextHash:             a.Locator.TextHash,
			Relation:             relation,
			Inferred:             a.Inferred,
			Verification:         string(a.Verification),
			Evidence:             refs(a.Evidence),
			VerificationEvidence: refs(a.VerificationEvidence),
			Diagnostics:          messages(a.Diagnostics),
		}
	}
	return out
}

// refs copies evidence references into the wire form.
func refs(in []model.EvidenceRef) []Ref {
	out := make([]Ref, len(in))
	for i, r := range in {
		out[i] = Ref{
			Path:       r.Path,
			Symbol:     r.Symbol,
			LineStart:  r.LineStart,
			LineEnd:    r.LineEnd,
			Kind:       string(r.Kind),
			Provenance: string(r.Provenance),
			Confidence: r.Confidence,
			Verifier:   r.Verifier,
		}
	}
	return out
}

// diffFiles summarises each changed path and keeps a readable number of lines.
func diffFiles(changes []repo.Change) []DiffFile {
	out := make([]DiffFile, len(changes))
	for i, c := range changes {
		file := DiffFile{Path: c.Path, Status: c.Status}
		for _, line := range analyze.Lines(c.Diff) {
			if line.Added {
				file.Added++
			} else {
				file.Removed++
			}
			if len(file.Lines) == maxDiffLines {
				file.Truncated++
				continue
			}
			kind := "removed"
			if line.Added {
				kind = "added"
			}
			file.Lines = append(file.Lines, DiffLine{Kind: kind, Line: line.Line, Text: line.Text})
		}
		out[i] = file
	}
	return out
}

// messages flattens diagnostics to the strings the view shows.
func messages(in []model.Diagnostic) []string {
	out := make([]string, len(in))
	for i, d := range in {
		if d.Path != "" {
			out[i] = d.Path + ": " + d.Message
			continue
		}
		out[i] = d.Message
	}
	return out
}
