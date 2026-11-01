// Command observe reports what the read-only repository reader and the goal
// baseline see. It is the thin startup layer from .agents/specs/architecture.md:
// flags and output only, no domain logic.
package main

import (
	"fmt"
	"os"

	"observe/internal/analyze"
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
	for _, s := range specs {
		if s.Lock.Family != "specs" || s.Lock.Type != "spec" {
			continue
		}
		fmt.Printf("goals        %d in %s\n", len(analyze.Requirements(s.Path, s.Body)), s.Path)
	}
	return nil
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
