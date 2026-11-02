// Package repo is the read-only repository reader described in
// .agents/specs/system-design.md (Components & ownership, Repo reader).
//
// The read-only guarantee is structural: this package has no write, delete,
// rename, or execute method, and never runs a command that is not a fixed,
// read-only git query. It refuses to read outside the repository root.
package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

// Repo is an open working tree.
type Repo struct {
	root     string // absolute, symlink-resolved working tree root
	identity string // absolute git common dir: a moved repo is a new identity
}

// Lock is the freeze marker at the top of a spec document.
type Lock struct {
	Family string // "specs" or "design"
	SHA    string
	Date   string // YYYY-MM-DD
	Type   string // "spec", "system-design", "architecture"; empty for design
}

// SpecFile is one locked Markdown document.
type SpecFile struct {
	Path string // repository-relative
	Lock Lock
	Body []byte
	Hash string // hex sha256 of Body
}

// lockMarker matches `<!-- specs:locked:<sha> <date> [type=<t>] -->`. The design
// family carries no type. A document whose marker does not parse is not a
// locked spec; it is reported as a diagnostic by the caller, never guessed.
var lockMarker = regexp.MustCompile(`^<!-- (specs|design):locked:([0-9a-f]{4,64}) (\d{4}-\d{2}-\d{2})(?: type=([a-z-]+))? -->$`)

// Open resolves path to a git working tree. It fails rather than inventing a
// repository: a directory that is not a working tree is a typed error, not an
// empty repository (system-design, Failure modes).
func Open(path string) (*Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", path, err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", path, err)
	}

	r := &Repo{root: abs}
	out, err := r.git("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not a git working tree: %w", abs, err)
	}
	r.root = strings.TrimSpace(string(out))

	out, err = r.git("rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("read repository identity: %w", err)
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(r.root, common)
	}
	if r.identity, err = filepath.EvalSymlinks(common); err != nil {
		return nil, fmt.Errorf("resolve repository identity: %w", err)
	}
	return r, nil
}

// Root is the absolute working tree root.
func (r *Repo) Root() string { return r.root }

// Identity distinguishes one repository from another. A repository that is
// moved or replaced is a new identity until it is re-opened and validated.
func (r *Repo) Identity() string { return r.identity }

// Commit is the current HEAD commit, or "" for a repository with no commits yet.
//
// Only the unborn-branch case is folded into "": `rev-parse --verify --quiet`
// exits 1 when the ref does not resolve yet. Any other failure is returned, so a
// broken repository cannot read as a clean one with no commits.
func (r *Repo) Commit() (string, error) {
	out, err := r.command("rev-parse", "--verify", "--quiet", "HEAD").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("read HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Files lists the repository's own files: every path in the index that is a file,
// plus the working tree's untracked files that git does not ignore.
//
// Work in progress is the normal case — a new source file exists on disk before it
// is committed — so reading only the index showed a TypeScript project as having no
// source at all. Ignored paths stay out: node_modules, build output, and the rest
// of what .gitignore already declares as not this project.
func (r *Repo) Files() ([]string, error) {
	tracked, err := r.TrackedFiles()
	if err != nil {
		return nil, err
	}
	out, err := r.git("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	untracked := splitNUL(out)

	seen := make(map[string]bool, len(tracked)+len(untracked))
	paths := make([]string, 0, len(tracked)+len(untracked))
	for _, group := range [][]string{tracked, untracked} {
		for _, path := range group {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// TrackedFiles lists repository-relative paths from the index, sorted.
//
// Gitlinks — nested worktrees and submodules, index mode 160000 — are omitted:
// git records them as index entries, but on disk they are directories that belong
// to another repository, so they are neither files to read nor part of this
// project's source.
func (r *Repo) TrackedFiles() ([]string, error) {
	out, err := r.git("ls-files", "-s", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range splitNUL(out) {
		// "<mode> <sha> <stage>	<path>"
		meta, path, found := strings.Cut(entry, "	")
		if !found || path == "" {
			continue
		}
		if strings.HasPrefix(meta, gitlinkMode) {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

// gitlinkMode is the index mode of an entry that is another repository.
const gitlinkMode = "160000"

// ReadFile reads one repository-relative file. Absolute paths and paths that
// escape the root are refused, so repository content cannot name a file
// elsewhere on the machine.
func (r *Repo) ReadFile(rel string) ([]byte, error) {
	clean, err := cleanRel(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(r.root, clean))
}

// LockedSpecs returns every tracked Markdown document carrying a lock marker,
// sorted by path. Documents without a marker are not locked specs.
func (r *Repo) LockedSpecs() ([]SpecFile, error) {
	tracked, err := r.TrackedFiles()
	if err != nil {
		return nil, err
	}
	var specs []SpecFile
	for _, path := range tracked {
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			continue
		}
		body, err := r.ReadFile(path)
		if err != nil {
			// A tracked but unreadable file is a real failure: it must not look
			// like a repository with no locked specs.
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		lock, ok := ParseLock(body)
		if !ok {
			continue
		}
		sum := sha256.Sum256(body)
		specs = append(specs, SpecFile{
			Path: path,
			Lock: lock,
			Body: body,
			Hash: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Path < specs[j].Path })
	return specs, nil
}

// Change is one path in the selected diff, with the content needed to attribute
// a change to the code it touched.
//
// The bodies are in-memory only: nothing here is stored or persisted
// (.agents/specs/observe-progress-view.md, Data requirements; AC12). A body is
// nil when that side of the change does not exist, so a deleted file has no New
// and an added one has no Old. Reading both sides is what lets the analyzer
// notice that a change removed the declaration a goal rests on; the diff alone
// cannot show that, because a removed declaration is absent from the working
// tree.
type Change struct {
	Path   string
	Status string // porcelain v1 XY code: " M" modified, "A " added, " D" deleted, "??" untracked, "R " renamed
	Diff   string // unified diff for this path; empty when git has none to show
	Old    []byte // content at HEAD; nil when the path did not exist there
	New    []byte // content in the working tree; nil when the file is gone
}

// Changes returns the selected diff split by changed path, plus its hash.
//
// A changed path that is a directory on disk — an untracked nested repository or
// worktree — has no line diff and cannot be read as a file. It is returned in
// skipped instead of failing the pass, because one such path must not cost the
// whole analysis.
//
// Only the hash is durable; the diff and both bodies are transient
// (.agents/specs/observe-progress-view.md, Data requirements). The hash covers
// the status listing and every per-path diff, so a change that alters either is
// visible in the input fingerprint.
//
// Every git invocation here is a fixed read-only query. --no-ext-diff and
// --no-textconv keep a repository's configured diff driver from running.
func (r *Repo) Changes() ([]Change, string, []string, error) {
	out, err := r.git("status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, "", nil, err
	}
	commit, err := r.Commit()
	if err != nil {
		return nil, "", nil, err
	}
	var skipped []string

	hashInput := append([]byte{}, out...)
	changes := make([]Change, 0, len(statusEntries(out)))
	for _, e := range statusEntries(out) {
		c := Change{Path: e.path, Status: e.code}
		untracked := e.code == "??"

		if untracked {
			// An untracked file has no HEAD version to diff against, so its whole
			// content is the addition. git diff --no-index reports it the same way as
			// any other addition; exit status 1 means "differences", which is expected.
			diff, err := r.gitAllowDiff("diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--", "/dev/null", e.path)
			if err != nil {
				return nil, "", nil, err
			}
			c.Diff = diff
		}

		if commit != "" && !untracked {
			diff, err := r.git("diff", "HEAD", "--no-ext-diff", "--no-textconv", "--no-color", "--", e.path)
			if err != nil {
				return nil, "", nil, err
			}
			c.Diff = string(diff)

			// A staged add has no HEAD version, so asking for one is a request
			// for a path that never existed. Every other status code implies the
			// path did exist, and a failure there is a real read error rather than
			// an absence.
			if !strings.Contains(e.code, "A") {
				oldPath := e.path
				if e.orig != "" {
					oldPath = e.orig // a rename's pre-image lives at the old path
				}
				old, err := r.git("show", "HEAD:"+oldPath)
				if err != nil {
					return nil, "", nil, err
				}
				c.Old = old
			}
		}

		if !strings.Contains(e.code, "D") {
			body, err := r.ReadFile(e.path)
			if errors.Is(err, syscall.EISDIR) {
				// git marks an untracked directory with a trailing slash; the path a
				// reader sees should not carry it.
				skipped = append(skipped, strings.TrimSuffix(e.path, "/"))
				continue
			}
			if err != nil {
				return nil, "", nil, err
			}
			c.New = body
		}

		hashInput = append(append(hashInput, []byte(c.Path)...), []byte(c.Diff)...)
		changes = append(changes, c)
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	sum := sha256.Sum256(hashInput)
	return changes, hex.EncodeToString(sum[:]), skipped, nil
}

// ParseLock reads the lock marker of a document. The marker must be its first
// non-empty line; a marker anywhere else is not a lock, so a spec quoted inside
// another document cannot masquerade as a locked one.
func ParseLock(body []byte) (Lock, bool) {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := lockMarker.FindStringSubmatch(line)
		if m == nil {
			return Lock{}, false
		}
		return Lock{Family: m[1], SHA: m[2], Date: m[3], Type: m[4]}, true
	}
	return Lock{}, false
}

// cleanRel validates a repository-relative path.
func cleanRel(rel string) (string, error) {
	if rel == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("refuse absolute path %q", rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refuse path outside the repository: %q", rel)
	}
	return clean, nil
}

// splitNUL splits NUL-terminated git output, dropping the trailing empty field.
func splitNUL(out []byte) []string {
	var paths []string
	for _, field := range strings.Split(string(out), "\x00") {
		if field != "" {
			paths = append(paths, field)
		}
	}
	return paths
}

// statusEntry is one record of `git status --porcelain=v1 -z`.
type statusEntry struct {
	code string // XY
	path string
	orig string // original path of a rename or copy; empty otherwise
}

// statusEntries parses `git status --porcelain=v1 -z`. A rename or copy entry is
// followed by the original path in its own field: recording it as the pre-image
// keeps it from being read as another changed path.
func statusEntries(out []byte) []statusEntry {
	var entries []statusEntry
	fields := splitNUL(out)
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 4 {
			continue
		}
		e := statusEntry{code: field[:2], path: field[3:]}
		if strings.ContainsAny(e.code, "RC") && i+1 < len(fields) {
			e.orig = fields[i+1]
			i++
		}
		entries = append(entries, e)
	}
	return entries
}
