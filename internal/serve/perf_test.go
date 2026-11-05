package serve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// AC6/Performance: one full refresh of a repository with 100,000 tracked lines
// completes within 3 seconds at p95. Until this test existed the budget had no
// fixture behind it, so it was a promise nobody had measured.
func TestRefreshOfOneHundredThousandLinesIsWithinBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("the 100k-line fixture is the point of this test")
	}

	dir, lines := largeFixture(t, 100_000)
	t.Logf("fixture: %d lines", lines)

	s := newServer(t, dir, "")
	ctx := context.Background()

	// Warm the parser and the page's own paths, then measure refreshes with a real
	// edit each time: the cache must not be what the timing rewards.
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	const samples = 5
	durations := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		edit(t, dir, fmt.Sprintf("// refresh %d\n", i))
		start := time.Now()
		if err := s.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		durations = append(durations, time.Since(start))
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[len(durations)-1]
	if len(durations) >= 20 {
		p95 = durations[int(float64(len(durations))*0.95)]
	}
	t.Logf("refresh p95 = %s over %d samples (min %s, max %s)",
		p95, samples, durations[0], durations[len(durations)-1])

	if p95 > 3*time.Second {
		t.Fatalf("p95 refresh = %s, budget is 3s", p95)
	}
}

// largeFixture writes a repository of roughly `want` tracked lines across ten
// modules, in both V1 languages, with a locked spec of goals and checks.
func largeFixture(t *testing.T, want int) (string, int) {
	t.Helper()

	files := map[string]string{}
	written := 0

	// Ten modules, grown until the line count is reached, so the fixture is the
	// size the budget names rather than whatever a fixed file count produced.
	for n := 0; written < want; n++ {
		m := n % 10
		var path, body string
		if n%5 == 0 {
			path = fmt.Sprintf("internal/m%d/t%d.ts", m, n)
			body = tsFile(m, n)
		} else {
			path = fmt.Sprintf("internal/m%d/f%d.go", m, n)
			body = goFile(m, n)
		}
		files[path] = body
		written += strings.Count(body, "\n")
	}

	// A goal spec with checks, so the analysis does the whole job: goals, checks,
	// declarations, structure, and the diff.
	files[".agents/specs/spec.md"] = specDoc

	return fixtureRepo(t, files), written
}

func goFile(module, index int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package m%d\n\nimport (\n\t\"fmt\"\n\n\t\"example/internal/m%d\"\n)\n\n", module, (module+1)%10)
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, `// Handle%d does a unit of work and returns its result.
func Handle%d(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("empty input %d")
	}
	value := m%d.Convert(input)
	if value == "" {
		return "", nil
	}
	return fmt.Sprintf("%%s-%%d", value, %d), nil
}

`, i, i, i, (module+1)%10, i)
	}
	fmt.Fprintf(&b, "var sink%d = %d\n", index, index)
	return b.String()
}

func tsFile(module, index int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "import { convert } from '../m%d/util';\n\n", (module+1)%10)
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, `export interface Shape%d {
  readonly id: string;
  readonly value: number;
}

export function handle%d(input: Shape%d): string {
  const value = convert(input.id);
  if (value.length === 0) {
    return String(input.value);
  }
  return value + String(%d);
}

`, i, i, i, i)
	}
	fmt.Fprintf(&b, "export const sink%d = %d;\n", index, index)
	return b.String()
}

// edit appends one line to a source file, so each refresh sees new input.
func edit(t *testing.T, dir, line string) {
	t.Helper()
	path := filepath.Join(dir, "internal/m1/f1.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, []byte("\n"+line)...), 0o644); err != nil {
		t.Fatal(err)
	}
}
