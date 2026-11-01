package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// command prepares one git invocation in the repository root.
//
// Each setting here is load-bearing for the trust boundary in
// .agents/specs/observe-progress-view.md (read-only, no repository code runs):
//
//   - GIT_OPTIONAL_LOCKS=0 stops `status` from refreshing and rewriting the
//     index, which would make AC10 (repository unchanged) fail.
//   - core.fsmonitor=false stops a repository from naming a program for git to
//     run while reading the index.
//   - GIT_EXTERNAL_DIFF="" (plus --no-ext-diff/--no-textconv at the call site)
//     stops a repository's configured diff driver from being executed.
//   - GIT_TERMINAL_PROMPT=0 stops any credential prompt.
func (r *Repo) command(args ...string) *exec.Cmd {
	full := append([]string{"-c", "core.fsmonitor=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(),
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_EXTERNAL_DIFF=",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
	)
	return cmd
}

// git runs one read-only git query and treats any non-zero exit as a failure.
func (r *Repo) git(args ...string) ([]byte, error) {
	cmd := r.command(args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
