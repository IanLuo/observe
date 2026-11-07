package serve

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"syscall"
	"time"

	"observe/internal/analyze"
	"observe/internal/model"
	"observe/internal/repo"
	"observe/internal/store"
)

// pipeline is the built-in analysis pass: read the repository, decide the
// baseline, consult the cache, analyze, and replace the cache record.
//
// The cache is read before the analysis runs, so an unchanged repository is not
// recomputed, and written only after a complete pass, so a failure never replaces
// a good record (AC9, AC12).
func (o Options) pipeline(ctx context.Context) (Analyzed, error) {
	if err := ctx.Err(); err != nil {
		return Analyzed{}, err
	}

	r, err := repo.Open(o.RepoPath)
	if err != nil {
		return Analyzed{}, err
	}
	commit, err := r.Commit()
	if err != nil {
		return Analyzed{}, err
	}
	tracked, err := r.Files()
	if err != nil {
		return Analyzed{}, err
	}
	changes, diffHash, skipped, err := r.Changes()
	if err != nil {
		return Analyzed{}, err
	}
	specs, err := r.LockedSpecs()
	if err != nil {
		return Analyzed{}, err
	}
	sources, sourceDiags, err := readSources(r, tracked)
	if err != nil {
		return Analyzed{}, err
	}

	docs := make([]analyze.Doc, len(specs))
	for i, s := range specs {
		docs[i] = analyze.Doc{Path: s.Path, Family: s.Lock.Family, Type: s.Lock.Type}
	}
	baseline := analyze.Baseline(docs)

	// The project's construction does not depend on a goal baseline, so it is read
	// before the baseline branch: a repository with a missing or conflicting
	// baseline can still be seen (AC7). Manifests join the pass because a workspace
	// package name is what makes a bare import resolvable.
	churn, churnErr := r.Churn()
	if churnErr != nil {
		churn = map[string]int{}
	}

	manifests, manifestDiags := readManifests(r, tracked)
	scanned := analyze.AnalyzeSources(append(append([]analyze.File{}, sources...), manifests...))
	declarations, structure := scanned.Declarations, scanned.Structure

	parseDiags := append(scanned.Diagnostics, sourceDiags...)
	parseDiags = append(parseDiags, manifestDiags...)
	if names := summaryNames(skipped); names != "" {
		parseDiags = append(parseDiags, model.Diagnostic{Op: "diff", Message: names})
	}

	fingerprint := analyze.Fingerprint(analyze.FingerprintInput{
		RepoIdentity:  r.Identity(),
		Commit:        commit,
		DiffHash:      diffHash,
		Specs:         specFiles(specs),
		Sources:       sources,
		SchemaVersion: store.SchemaVersion,
	})

	out := Analyzed{
		Repository: r.Root(),
		Baseline:   baseline,
		Structure:  structure,
		Changes:    changes,
		Churn:      churn,
		Snapshot: model.Snapshot{
			Fingerprint: fingerprint,
			Generation:  fingerprint.Generation(),
			Freshness:   model.FreshnessCurrent,
			Diagnostics: parseDiags,
		},
	}

	cache, handle, cacheDiags := o.openCache(r.Root())
	out.Cache = cache
	out.Snapshot.Diagnostics = append(out.Snapshot.Diagnostics, cacheDiags...)
	if handle != nil {
		defer handle.Close()
	}

	// A repository without a usable baseline has no goals to assess, but it still
	// gets a snapshot: browsing stays available and alignment stays unavailable
	// (AC7). A prompt can never become the baseline.
	// Every locked document may declare checks. The goal spec supplies the goals; the
	// others supply invariants, which are shown with their own verdicts and never
	// counted as goals (AC14).
	docBodies := make([]analyze.SpecDoc, 0, len(specs))
	for _, spec := range specs {
		docBodies = append(docBodies, analyze.SpecDoc{Path: spec.Path, Body: spec.Body})
	}
	baselinePath := ""
	if baseline.State == model.BaselineAvailable {
		baselinePath = baseline.SpecPaths[0]
	}

	if baseline.State == model.BaselineAvailable {
		if handle != nil {
			loaded, err := handle.Load(fingerprint)
			switch {
			case err != nil:
				out.Cache.State = "unreadable"
				out.Snapshot.Diagnostics = append(out.Snapshot.Diagnostics, model.Diagnostic{
					Op: "cache", Message: "cache unreadable: " + err.Error(),
				})
			case loaded.Fresh:
				out.Snapshot.Assessments = loaded.Snapshot.Assessments
				out.Snapshot.CreatedAt = loaded.Snapshot.CreatedAt
				out.Cache.State = "current"
				return out, nil
			default:
				out.Snapshot.Diagnostics = append(out.Snapshot.Diagnostics, loaded.Diagnostics...)
			}
		}
	}

	result := analyze.Analyze(analyze.Input{
		Baseline:  baselinePath,
		Docs:      docBodies,
		Sources:   sources,
		Decls:     declarations,
		Structure: structure,
		Changes:   changesOf(changes),
		Coverage:  analyze.Measure(tracked),
		Prompt:    o.Prompt,
	})
	out.Snapshot.Assessments = result.Assessments
	out.Snapshot.Diagnostics = append(out.Snapshot.Diagnostics, result.Diagnostics...)
	if len(result.PromptConflicts) > 0 {
		out.Conflicts = result.PromptConflicts
	}

	out.Snapshot.CreatedAt = time.Now().UTC()
	if handle != nil {
		if err := handle.Save(out.Snapshot); err != nil {
			out.Cache.State = "unsaved"
			out.Snapshot.Diagnostics = append(out.Snapshot.Diagnostics, model.Diagnostic{
				Op: "cache", Message: "cache not saved: " + err.Error(),
			})
			return out, nil
		}
		out.Cache.State = "saved"
	}
	return out, nil
}

// openCache prepares the optional cache. A cache that cannot be opened is a
// diagnostic, never a failed analysis: browsing must survive a broken cache
// (system-design, Failure modes).
func (o Options) openCache(root string) (Cache, *store.Cache, []model.Diagnostic) {
	if o.CachePath == "" {
		return Cache{State: "disabled"}, nil, nil
	}
	handle, err := store.Open(o.CachePath, root)
	if err != nil {
		return Cache{State: "unavailable"}, nil, []model.Diagnostic{{
			Op: "cache", Message: "cache unavailable: " + err.Error(),
		}}
	}
	return Cache{Path: handle.Path(), State: "absent"}, handle, nil
}

// summaryNames describes changed paths that are directories on disk, naming a few
// and counting the rest: a nested repository is one fact about the diff.
func summaryNames(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	shown := paths
	if len(shown) > 3 {
		shown = shown[:3]
	}
	verb := "are"
	if len(paths) == 1 {
		verb = "is"
	}
	message := fmt.Sprintf(
		"%d changed path%s a directory, not a file (nested repository or worktree): %s",
		len(paths), " "+verb, strings.Join(shown, ", "),
	)
	if len(paths) > len(shown) {
		message += fmt.Sprintf(", and %d more", len(paths)-len(shown))
	}
	return message + "; skipped"
}

// readManifests reads the package manifests of the project's own files. A
// workspace imports its siblings by package name, so resolution needs these even
// though a manifest is not source.
func readManifests(r *repo.Repo, tracked []string) ([]analyze.File, []model.Diagnostic) {
	var (
		files []analyze.File
		diags []model.Diagnostic
	)
	for _, p := range tracked {
		if path.Base(p) != "package.json" || analyze.OutOfScope(p) {
			continue
		}
		body, err := r.ReadFile(p)
		if err != nil {
			diags = append(diags, model.Diagnostic{Op: "manifest", Path: p, Message: "unreadable: " + err.Error()})
			continue
		}
		files = append(files, analyze.File{Path: p, Body: body})
	}
	return files, diags
}

// readSources reads the tracked files in the V1 language set. A tracked file that
// cannot be read is an error, never a missing file.
func readSources(r *repo.Repo, tracked []string) ([]analyze.File, []model.Diagnostic, error) {
	var (
		files []analyze.File
		diags []model.Diagnostic
	)
	for _, p := range tracked {
		if !analyze.IsSourcePath(p) {
			continue
		}
		body, err := r.ReadFile(p)
		if errors.Is(err, syscall.EISDIR) {
			// The index says file, the working tree says directory — a nested
			// repository or a replaced path. It is not readable source, and one odd
			// path must not fail the whole analysis.
			diags = append(diags, model.Diagnostic{
				Op:      "source",
				Path:    p,
				Message: "tracked path is a directory, not a file; skipped",
			})
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", p, err)
		}
		files = append(files, analyze.File{Path: p, Body: body})
	}
	return files, diags, nil
}

// specFiles is the locked-spec set in the form the fingerprint hashes.
func specFiles(specs []repo.SpecFile) []analyze.File {
	out := make([]analyze.File, len(specs))
	for i, s := range specs {
		out[i] = analyze.File{Path: s.Path, Body: s.Body}
	}
	return out
}

// specBody is one locked spec's body, or nil when it is not among the loaded ones.
func specBody(specs []repo.SpecFile, path string) []byte {
	for _, s := range specs {
		if s.Path == path {
			return s.Body
		}
	}
	return nil
}

// changesOf converts the reader's changes to the analyzer's view.
func changesOf(changes []repo.Change) []analyze.Change {
	out := make([]analyze.Change, len(changes))
	for i, c := range changes {
		out[i] = analyze.Change{Path: c.Path, Status: c.Status, Diff: c.Diff, Old: c.Old, New: c.New}
	}
	return out
}
