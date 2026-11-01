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

// TrackedFiles lists repository-relative paths from the index, sorted.
func (r *Repo) TrackedFiles() ([]string, error) {
	out, err := r.git("ls-files", "-z")
	if err != nil {
		return nil, err
	}
	paths := splitNUL(out)
	sort.Strings(paths)
	return paths, nil
}

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

// WorkingTree returns the changed paths and the hash of the selected diff.
//
// Only the hash leaves this function: the diff itself is never stored or
// persisted (.agents/specs/observe-progress-view.md, Data requirements). The
// hash covers both the status listing and the diff text so that a change which
// alters either is visible in the fingerprint.
func (r *Repo) WorkingTree() (paths []string, hash string, err error) {
	out, err := r.git("status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, "", err
	}
	paths = statusPaths(out)

	commit, err := r.Commit()
	if err != nil {
		return nil, "", err
	}
	var diff []byte
	if commit != "" {
		// --no-ext-diff and --no-textconv keep a repository's configured diff
		// driver from running.
		if diff, err = r.git("diff", "HEAD", "--no-ext-diff", "--no-textconv", "--no-color"); err != nil {
			return nil, "", err
		}
	}

	sum := sha256.Sum256(append(append([]byte{}, out...), diff...))
	return paths, hex.EncodeToString(sum[:]), nil
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

// statusPaths extracts changed paths from `git status --porcelain=v1 -z`. A
// rename or copy entry is followed by the original path as its own field, which
// is skipped rather than counted as another change.
func statusPaths(out []byte) []string {
	var paths []string
	fields := splitNUL(out)
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		paths = append(paths, entry[3:])
		if entry[0] == 'R' || entry[0] == 'C' {
			i++ // the original path
		}
	}
	return paths
}
