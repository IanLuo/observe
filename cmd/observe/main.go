// Command observe reports what the read-only repository reader, the goal
// baseline, and the analyzer see. It is the thin startup layer from
// .agents/specs/architecture.md: flags and output only, no domain logic.
package main

import (
	"fmt"
	"os"
	"strings"

	"observe/internal/analyze"
	"observe/internal/model"
	"observe/internal/repo"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: observe <repository>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "observe:", err)
		os.Exit(1)
	}
}

func run(path string) error {
	r, err := repo.Open(path)
	if err != nil {
		return err
	}

	commit, err := r.Commit()
	if err != nil {
		return err
	}
	tracked, err := r.TrackedFiles()
	if err != nil {
		return err
	}
	changed, diffHash, err := r.WorkingTree()
	if err != nil {
		return err
	}
	specs, err := r.LockedSpecs()
	if err != nil {
		return err
	}

	docs := make([]analyze.Doc, len(specs))
	for i, s := range specs {
		docs[i] = analyze.Doc{Path: s.Path, Family: s.Lock.Family, Type: s.Lock.Type}
	}
	baseline := analyze.Baseline(docs)

	fmt.Printf("repository   %s\n", r.Root())
	fmt.Printf("identity     %s\n", r.Identity())
	fmt.Printf("commit       %s\n", orNone(commit))
	fmt.Printf("tracked      %d files\n", len(tracked))
	fmt.Printf("working tree %d changed, diff sha256:%s\n", len(changed), short(diffHash))
	fmt.Printf("locked specs %d\n", len(specs))
	for _, s := range specs {
		fmt.Printf("             %s (%s)\n", s.Path, describeType(s.Lock))
	}
	fmt.Printf("baseline     %s\n", baseline.State)
	for _, d := range baseline.Diagnostics {
		fmt.Printf("             ! %s\n", d.Message)
	}
	if baseline.State != model.BaselineAvailable {
		fmt.Print("goals        unavailable: goal alignment needs exactly one locked spec\n")
		return nil
	}

	specPath := baseline.SpecPaths[0]
	var specBody []byte
	for _, s := range specs {
		if s.Path == specPath {
			specBody = s.Body
		}
	}

	files, err := readSources(r, tracked)
	if err != nil {
		return err
	}
	declarations, parseDiags := analyze.Declarations(files)
	fmt.Printf("sources      %d files, %d declarations, %d diagnostics\n", len(files), len(declarations), len(parseDiags))
	for _, d := range parseDiags {
		fmt.Printf("             ! %s: %s\n", d.Path, d.Message)
	}

	goals := analyze.Requirements(specPath, specBody)
	assessments := analyze.Assess(goals, declarations)
	fmt.Printf("goals        %d in %s\n", len(assessments), specPath)
	for _, a := range assessments {
		fmt.Printf("             %-9s links=%-3d tests=%-3d %s\n",
			a.Relation, len(a.Evidence), len(a.VerificationEvidence), truncate(a.Locator.Label, 64))
		if line := describeEvidence(a); line != "" {
			fmt.Println(line)
		}
	}
	return nil
}

// readSources reads the tracked files in the V1 language set. A tracked file
// that cannot be read is an error, never a missing file.
func readSources(r *repo.Repo, tracked []string) ([]analyze.File, error) {
	var files []analyze.File
	for _, p := range tracked {
		if !analyze.IsSourcePath(p) {
			continue
		}
		body, err := r.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		files = append(files, analyze.File{Path: p, Body: body})
	}
	return files, nil
}

// describeEvidence lists the strongest links for a goal, so an inferred link is
// visible rather than counted.
func describeEvidence(a model.GoalAssessment) string {
	if len(a.Evidence) == 0 {
		return ""
	}
	shown := a.Evidence
	if len(shown) > 3 {
		shown = shown[:3]
	}
	parts := make([]string, len(shown))
	for i, e := range shown {
		parts[i] = fmt.Sprintf("%s:%d %s", e.Path, e.LineStart, e.Symbol)
	}
	return "                      inferred: " + strings.Join(parts, ", ") + "\n"
}

// describeType names a lock's document type, which the design family does not carry.
func describeType(lock repo.Lock) string {
	if lock.Type == "" {
		return "no type"
	}
	return "type=" + lock.Type
}

func orNone(s string) string {
	if s == "" {
		return "(no commits)"
	}
	return s
}

func short(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
