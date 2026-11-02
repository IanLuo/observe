// Command observe starts the loopback application (.agents/specs/architecture.md
// D1). It is the thin startup layer: flags, process lifecycle, and opening the
// browser. Reading the repository, analysis, caching, and presentation live in
// internal/serve, which is the composition root.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"observe/internal/repo"
	"observe/internal/serve"
	"observe/internal/store"
)

// portBase and portTries shape the automatic port choice: start at 8080 and walk
// up, so the first project gets a memorable address and the next one a neighbour.
const (
	portBase  = 8080
	portTries = 50
)

func main() {
	cacheDefault, cacheNote := store.DefaultPath()
	if cacheNote != nil {
		cacheDefault = "" // no cache is a valid configuration; the note is printed below
	}

	prompt := flag.String("prompt", "", "optional task prompt; context only, never a replacement for a locked spec")
	cachePath := flag.String("cache", cacheDefault, "cache file outside the repository; empty disables the cache")
	addr := flag.String("addr", "", "exact loopback address to bind; empty picks the first free port from 8080")
	report := flag.Bool("report", false, "print the state as JSON and exit instead of serving")
	noOpen := flag.Bool("no-open", false, "do not open a browser")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: observe [-prompt \"text\"] [-cache <path>] [-addr <host:port>] [-report] [-no-open] <repository>")
		fmt.Fprintln(os.Stderr, "       observe stop [<path>]   stop the instance serving that project (default: this directory)")
		flag.PrintDefaults()
	}
	flag.Parse()

	// `observe stop` is a lifecycle command, not a repository named "stop": a path
	// is resolved instead of analysed, so it works from any directory in a project.
	if flag.Arg(0) == "stop" {
		target := flag.Arg(1)
		if target == "" {
			target = "."
		}
		if err := stopProject(target); err != nil {
			fmt.Fprintln(os.Stderr, "observe:", err)
			os.Exit(1)
		}
		return
	}

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if cacheNote != nil {
		fmt.Fprintln(os.Stderr, "observe: cache disabled:", cacheNote)
	}

	opts := serve.Options{RepoPath: flag.Arg(0), Prompt: *prompt, CachePath: *cachePath}
	if err := run(opts, *addr, *report, *noOpen); err != nil {
		fmt.Fprintln(os.Stderr, "observe:", err)
		os.Exit(1)
	}
}

// run serves until interrupted, or prints one state document in report mode.
func run(opts serve.Options, addr string, report, noOpen bool) error {
	s, err := serve.New(opts)
	if err != nil {
		return err
	}

	if report {
		refreshErr := s.Refresh(context.Background())
		document := map[string]any{"ok": refreshErr == nil, "data": s.State()}
		if refreshErr != nil {
			document["error"] = refreshErr.Error()
		}
		encoded, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return refreshErr
	}

	// One instance per project: a second start reports the running one instead of
	// failing on a busy port or shadowing it with a second analysis.
	root, err := repo.Open(opts.RepoPath)
	if err != nil {
		return err
	}
	if record, found, err := loadRun(root.Root()); err != nil {
		return err
	} else if found && alive(record.PID) {
		fmt.Printf("already serving %s at %s (pid %d); stop it with `observe stop`\n", root.Root(), record.URL, record.PID)
		return nil
	}

	var url string
	if addr != "" {
		url, err = s.Listen(addr)
	} else {
		url, err = freeAddr(s, portBase, portTries)
	}
	if err != nil {
		return err
	}
	fmt.Printf("observe %s\n", url)

	record := runRecord{
		Path:      root.Root(),
		URL:       url,
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	record.Port = urlPort(url)
	if err := saveRun(record); err != nil {
		fmt.Fprintln(os.Stderr, "observe:", err)
	}
	defer removeRun(root.Root())

	// The page is opened before the first analysis and polls while it runs, so a
	// slow repository shows progress instead of an empty window and a silent
	// terminal.
	if !noOpen {
		openBrowser(url)
	}
	fmt.Printf("analysing %s\n", record.Path)

	// A failed first analysis is reported by the view, not by exiting: the page
	// stays available and offers retry (AC9).
	if err := s.Refresh(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "observe: first analysis failed: %v\n", err)
	} else {
		fmt.Println("ready")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Close(ctx)
}

// openBrowser opens the loopback URL with the platform's opener. It runs a system
// utility, never repository code.
func openBrowser(url string) {
	opener := ""
	switch runtime.GOOS {
	case "darwin":
		opener = "open"
	case "linux":
		opener = "xdg-open"
	default:
		return
	}
	if err := exec.Command(opener, url).Start(); err != nil {
		fmt.Fprintf(os.Stderr, "observe: could not open a browser: %v\n", err)
	}
}
