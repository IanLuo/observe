// Command observe reports what the read-only repository reader, the goal
// baseline, and the analyzer see. It is the thin startup layer from
// .agents/specs/architecture.md: flags and output only, no domain logic.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"observe/internal/analyze"
	"observe/internal/model"
	"observe/internal/repo"
	"observe/internal/store"
)

func main() {
	cacheDefault, cacheNote := store.DefaultPath()
	if cacheNote != nil {
		cacheDefault = "" // no cache is a valid configuration; say so below
	}
	prompt := flag.String("prompt", "", "optional task prompt; context only, never a replacement for a locked spec")
	cachePath := flag.String("cache", cacheDefault, "cache file path outside the repository; empty disables the cache")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: observe [-prompt \"text\"] [-cache <path>] <repository>")
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if cacheNote != nil {
		fmt.Fprintln(os.Stderr, "observe: cache disabled:", cacheNote)
	}
	if err := run(flag.Arg(0), *prompt, *cachePath); err != nil {
		fmt.Fprintln(os.Stderr, "observe:", err)
		os.Exit(1)
	}
}

func run(path, prompt, cachePath string) error {
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
	changed, diffHash, err := r.Changes()
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
	describeDiff(changed)
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
	reported := make(map[string]bool, len(parseDiags))
	for _, d := range parseDiags {
		fmt.Printf("             ! %s: %s\n", d.Path, d.Message)
		reported[d.Path+"|"+d.Op+"|"+d.Message] = true
	}

	goals := analyze.Requirements(specPath, specBody)
	cached := loadCache(cachePath, r, fingerprintOf(r, commit, diffHash, specs, files))
	defer cached.close()

	// Prompt conflicts are report-only and are never persisted, so they are
	// computed on both the cached and the fresh path.
	conflicts := analyze.PromptConflicts(goals, prompt)

	if cached.snapshot != nil {
		reportGoals(cached.snapshot.Assessments)
		reportPrompt(conflicts)
		return nil
	}

	changes := make([]analyze.Change, len(changed))
	for i, c := range changed {
		changes[i] = analyze.Change{Path: c.Path, Status: c.Status, Diff: c.Diff, Old: c.Old, New: c.New}
	}
	result := analyze.Analyze(goals, declarations, changes, prompt)
	// A changed file is parsed again on both sides of the diff, so a parse
	// diagnostic can arrive twice; report each one once.
	for _, d := range result.Diagnostics {
		key := d.Path + "|" + d.Op + "|" + d.Message
		if reported[key] {
			continue
		}
		reported[key] = true
		fmt.Printf("             ! %s: %s\n", d.Path, d.Message)
	}
	reportGoals(result.Assessments)
	reportPrompt(conflicts)

	saved := model.Snapshot{
		Fingerprint: cached.fingerprint,
		Generation:  cached.fingerprint.Generation(),
		CreatedAt:   time.Now().UTC(),
		Freshness:   model.FreshnessCurrent,
		Assessments: result.Assessments,
		Diagnostics: result.Diagnostics,
	}
	cached.save(saved)
	return nil
}

// reportGoals prints one line per goal: its relation to the selected diff, the
// evidence behind it, and any diagnostic that explains the classification.
func reportGoals(assessments []model.GoalAssessment) {
	spec := ""
	if len(assessments) > 0 {
		spec = " in " + assessments[0].Locator.SpecPath
	}
	fmt.Printf("goals        %d%s\n", len(assessments), spec)
	for _, a := range assessments {
		fmt.Printf("             %-19s links=%-3d tests=%-3d %s\n",
			a.Relation, len(a.Evidence), len(a.VerificationEvidence), truncate(a.Locator.Label, 64))
		if line := describeEvidence(a); line != "" {
			fmt.Println(line)
		}
		for _, d := range a.Diagnostics {
			if d.Op == "diff" || d.Op == "link" {
				fmt.Printf("                      ! %s\n", d.Message)
			}
		}
	}
}

// reportPrompt prints the prompt conflicts, each with the sentence that caused it.
// The locked spec is never replaced by a prompt, so nothing here resolves.
func reportPrompt(conflicts []model.PromptConflict) {
	if len(conflicts) == 0 {
		return
	}
	fmt.Printf("prompt       %d conflict(s), unresolved; the locked spec remains the baseline\n", len(conflicts))
	for _, c := range conflicts {
		fmt.Printf("conflict     %q vs %s\n             ! %s\n", truncate(c.Prompt, 60), c.Locator.SpecPath, c.Diagnostic)
	}
}

// describeDiff shows the selected diff: what changed in each path, and the lines
// themselves, so a classification can be read against the change it judges.
func describeDiff(changes []repo.Change) {
	for _, c := range changes {
		lines := analyze.Lines(c.Diff)
		added, removed := 0, 0
		for _, l := range lines {
			if l.Added {
				added++
			} else {
				removed++
			}
		}
		suffix := ""
		if c.Diff == "" {
			suffix = " (no diff text: untracked or binary)"
		}
		fmt.Printf("             %s %s +%d/-%d%s\n", c.Status, c.Path, added, removed, suffix)
		shown := 0
		for _, l := range lines {
			if shown == maxDiffLinesShown {
				fmt.Printf("                 ... %d more changed lines\n", len(lines)-shown)
				break
			}
			mark := "-"
			if l.Added {
				mark = "+"
			}
			fmt.Printf("                 %s %s\n", mark, truncate(l.Text, 72))
			shown++
		}
	}
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

// maxDiffLinesShown bounds the diff text printed per changed path.
const maxDiffLinesShown = 8

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

// cache is the optional current-analysis cache. A nil handle means the cache is
// disabled or unusable, and analysis still runs: a cache failure must not block
// browsing (system-design, Failure modes).
type cache struct {
	handle      *store.Cache
	fingerprint model.InputFingerprint
	snapshot    *model.Snapshot
}

func (c *cache) close() {
	if c.handle != nil {
		c.handle.Close()
	}
}

// save replaces the current record. A failed write is reported and does not fail
// the run: the analysis is still valid, it is simply not remembered.
func (c *cache) save(s model.Snapshot) {
	if c.handle == nil {
		return
	}
	if err := c.handle.Save(s); err != nil {
		fmt.Printf("cache        NOT SAVED: %v\n", err)
		return
	}
	fmt.Printf("cache        %s, record replaced\n", c.handle.Path())
}

// loadCache opens the cache and reports its state. It sets snapshot only when the
// stored record matches the current fingerprint exactly; otherwise the record has
// been discarded and the caller recomputes.
func loadCache(path string, r *repo.Repo, fingerprint model.InputFingerprint) *cache {
	c := &cache{fingerprint: fingerprint}
	if path == "" {
		fmt.Printf("cache        disabled\n")
		return c
	}

	handle, err := store.Open(path, r.Root())
	if err != nil {
		fmt.Printf("cache        unavailable, analysis continues: %v\n", err)
		return c
	}
	c.handle = handle

	loaded, err := handle.Load(fingerprint)
	if err != nil {
		fmt.Printf("cache        %s, unreadable: %v\n", handle.Path(), err)
		return c
	}
	for _, d := range loaded.Diagnostics {
		fmt.Printf("cache        %s\n             ! %s\n", handle.Path(), d.Message)
	}
	if loaded.Fresh {
		c.snapshot = &loaded.Snapshot
		fmt.Printf("cache        %s, current (written %s)\n", handle.Path(), loaded.Snapshot.CreatedAt.Format(time.RFC3339))
		return c
	}
	if len(loaded.Diagnostics) == 0 {
		fmt.Printf("cache        %s, no record yet\n", handle.Path())
	}
	return c
}

// fingerprintOf records what the analysis depends on, as hashes. The task prompt
// is not part of it: a prompt is transient context, never a cache input (AC12).
func fingerprintOf(r *repo.Repo, commit, diffHash string, specs []repo.SpecFile, sources []analyze.File) model.InputFingerprint {
	specFiles := make([]analyze.File, len(specs))
	for i, s := range specs {
		specFiles[i] = analyze.File{Path: s.Path, Body: s.Body}
	}
	return analyze.Fingerprint(analyze.FingerprintInput{
		RepoIdentity:  r.Identity(),
		Commit:        commit,
		DiffHash:      diffHash,
		Specs:         specFiles,
		Sources:       sources,
		SchemaVersion: store.SchemaVersion,
	})
}
