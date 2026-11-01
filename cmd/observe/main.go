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

	"observe/internal/serve"
	"observe/internal/store"
)

func main() {
	cacheDefault, cacheNote := store.DefaultPath()
	if cacheNote != nil {
		cacheDefault = "" // no cache is a valid configuration; the note is printed below
	}

	prompt := flag.String("prompt", "", "optional task prompt; context only, never a replacement for a locked spec")
	cachePath := flag.String("cache", cacheDefault, "cache file outside the repository; empty disables the cache")
	addr := flag.String("addr", "127.0.0.1:0", "loopback listen address; a non-loopback address is refused")
	report := flag.Bool("report", false, "print the state as JSON and exit instead of serving")
	noOpen := flag.Bool("no-open", false, "do not open a browser")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: observe [-prompt \"text\"] [-cache <path>] [-addr <host:port>] [-report] [-no-open] <repository>")
		flag.PrintDefaults()
	}
	flag.Parse()
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

	url, err := s.Listen(addr)
	if err != nil {
		return err
	}
	fmt.Printf("observe %s\n", url)

	// A failed first analysis is reported by the view, not by exiting: the page
	// stays available and offers retry (AC9).
	if err := s.Refresh(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "observe: first analysis failed: %v\n", err)
	}

	if !noOpen {
		openBrowser(url)
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
