package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"observe/internal/repo"
)

// A running instance is recorded beside the cache, never inside the repository:
// the analysed project stays read-only, and the record is disposable state about
// this machine, not about the analysis (.agents/specs/observe-progress-view.md,
// Data requirements).
//
// The key is the repository root, so `observe stop` works from any directory
// inside a project, not only from the one that started it.
type runRecord struct {
	Path      string `json:"path"`
	Port      int    `json:"port"`
	URL       string `json:"url"`
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
}

// runDir is where instance records live: the user cache directory, beside the
// analysis cache.
func runDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate the user cache directory: %w", err)
	}
	dir := filepath.Join(base, "observe", "runs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

// runPath names the record for one repository root.
func runPath(root string) (string, error) {
	dir, err := runDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(dir, hex.EncodeToString(sum[:8])+".json"), nil
}

func saveRun(record runRecord) error {
	path, err := runPath(record.Path)
	if err != nil {
		return err
	}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("record the running instance: %w", err)
	}
	return nil
}

func loadRun(root string) (runRecord, bool, error) {
	path, err := runPath(root)
	if err != nil {
		return runRecord{}, false, err
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return runRecord{}, false, nil
	}
	if err != nil {
		return runRecord{}, false, err
	}
	var record runRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return runRecord{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	return record, true, nil
}

func removeRun(root string) {
	if path, err := runPath(root); err == nil {
		os.Remove(path)
	}
}

// alive reports whether the recorded process still exists. Signal 0 checks for
// existence without delivering anything.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// stopProject stops the instance serving the repository that contains dir.
func stopProject(dir string) error {
	r, err := repo.Open(dir)
	if err != nil {
		return err
	}
	root := r.Root()

	record, found, err := loadRun(root)
	if err != nil {
		return err
	}
	if !found {
		fmt.Printf("no running instance recorded for %s\n", root)
		return nil
	}
	if !alive(record.PID) {
		removeRun(root)
		fmt.Printf("no running instance for %s (removed a stale record)\n", root)
		return nil
	}

	if err := syscall.Kill(record.PID, syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop pid %d: %w", record.PID, err)
	}
	// Wait for the process to go, so a restart right after a stop finds the port
	// free.
	deadline := time.Now().Add(5 * time.Second)
	for alive(record.PID) {
		if time.Now().After(deadline) {
			return fmt.Errorf("pid %d is still running; it did not stop within 5s", record.PID)
		}
		time.Sleep(50 * time.Millisecond)
	}
	removeRun(root)
	fmt.Printf("stopped %s (pid %d, was %s)\n", root, record.PID, record.URL)
	return nil
}

// urlPort is the port in a "http://host:port" address, or 0 when it is unreadable.
func urlPort(raw string) int {
	parsed, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return 0
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return number
}

// freeAddr walks up from base until a loopback port is free. Auto-increment is
// deliberate: a stable first choice, a predictable neighbour when it is taken, and
// no port arithmetic for the user.
func freeAddr(s interface {
	Listen(string) (string, error)
}, base, tries int) (string, error) {
	var lastErr error
	for port := base; port < base+tries; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		url, err := s.Listen(addr)
		if err == nil {
			return url, nil
		}
		lastErr = err
		if !errors.Is(err, syscall.EADDRINUSE) {
			return "", err
		}
	}
	return "", fmt.Errorf("no free port in %d-%d: %w", base, base+tries-1, lastErr)
}
