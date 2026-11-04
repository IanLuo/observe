package analyze

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The selected diff, as text. It is parsed here so the view and the analyzer read
// the same lines, and no package has to parse a unified diff twice.

type Change struct {
	Path   string
	Status string // porcelain v1 XY code: " M", "A ", " D", "??", "R "
	Diff   string
	Old    []byte // content at HEAD; nil when the path did not exist there
	New    []byte // content in the working tree; nil when the file is gone
}

// DiffLine is one added or removed line of a unified diff, numbered on the side
// it belongs to. Line numbers are what attribute a change to a declaration.
type DiffLine struct {
	Line  int // 1-based, in the old file for a removal and the new file for an addition
	Text  string
	Added bool
}

// hunkHeader captures the old and new start lines of a hunk.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// Lines parses the added and removed lines out of unified diff text.
//
// Only hunk bodies count, so the `---`/`+++` header lines are not mistaken for a
// removal and an addition of the file itself. When the diff carries no hunk (an
// untracked file, a binary file), the result is empty: the caller has no line
// evidence and must not invent any.
func Lines(diff string) []DiffLine {
	var out []DiffLine
	oldLine, newLine := 0, 0
	inHunk := false

	for _, line := range strings.Split(diff, "\n") {
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			newLine, _ = strconv.Atoi(m[2])
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		switch line[0] {
		case ' ':
			oldLine++
			newLine++
		case '-':
			out = append(out, DiffLine{Line: oldLine, Text: line[1:]})
			oldLine++
		case '+':
			out = append(out, DiffLine{Line: newLine, Text: line[1:], Added: true})
			newLine++
		case '\\':
			// "\ No newline at end of file" carries no line of its own.
		default:
			inHunk = false // the next file's header
		}
	}
	return out
}

// plural picks the matching word for a count, so a diagnostic reads as English.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
