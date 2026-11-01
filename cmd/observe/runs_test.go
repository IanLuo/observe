package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// busyListener reports EADDRINUSE for the ports it was given and a URL otherwise.
type busyListener struct {
	busy map[int]bool
	used []string
}

func (b *busyListener) Listen(addr string) (string, error) {
	b.used = append(b.used, addr)
	if b.busy[urlPort("http://"+addr)] {
		// Wrap the real errno, as serve.Listen does, so the retry rule is the one
		// the product uses.
		return "", fmt.Errorf("listen on %s: %w", addr, syscall.EADDRINUSE)
	}
	return "http://" + addr, nil
}

// A taken port must not be an error the user has to resolve: the next one is used.
func TestFreeAddrWalksUpFromTheBasePort(t *testing.T) {
	listener := &busyListener{busy: map[int]bool{8080: true, 8081: true}}
	url, err := freeAddr(listener, 8080, 5)
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://127.0.0.1:8082" {
		t.Fatalf("picked %q, want 8082", url)
	}
	if len(listener.used) != 3 {
		t.Fatalf("tried %d addresses, want 3: %v", len(listener.used), listener.used)
	}
}

func TestFreeAddrGivesUpWithAUsefulError(t *testing.T) {
	listener := &busyListener{busy: map[int]bool{8080: true, 8081: true}}
	if _, err := freeAddr(listener, 8080, 2); err == nil {
		t.Fatal("a full range reported a port")
	}
}

// A failure that is not a busy port must not be retried on the next port.
func TestFreeAddrStopsOnAnUnrelatedFailure(t *testing.T) {
	failing := listenerFunc(func(addr string) (string, error) {
		return "", errors.New("listen on " + addr + ": operation not permitted")
	})
	if _, err := freeAddr(failing, 8080, 10); err == nil {
		t.Fatal("an unrelated failure was swallowed")
	}
}

type listenerFunc func(string) (string, error)

func (f listenerFunc) Listen(addr string) (string, error) { return f(addr) }

// The record is keyed by repository root, so a stop from a subdirectory finds the
// instance a start from the root recorded.
func TestRunRecordRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	root := fixtureRepo(t)
	record := runRecord{Path: root, Port: 8083, URL: "http://127.0.0.1:8083", PID: os.Getpid(), StartedAt: "2026-11-01T00:00:00Z"}
	if err := saveRun(record); err != nil {
		t.Fatal(err)
	}

	loaded, found, err := loadRun(root)
	if err != nil || !found {
		t.Fatalf("load: %v found=%v", err, found)
	}
	if loaded.URL != record.URL || loaded.PID != record.PID || loaded.Port != 8083 {
		t.Fatalf("record did not round-trip: %+v", loaded)
	}
	if mode := fileMode(t, root); mode != 0o600 {
		t.Errorf("record mode = %v, want 0600", mode)
	}

	removeRun(root)
	if _, found, err := loadRun(root); err != nil || found {
		t.Fatalf("record survived removal: found=%v err=%v", found, err)
	}
}

// A record whose process is gone must not claim an instance, and must not be left
// behind to confuse the next start.
func TestStopWithoutALiveProcessClearsTheRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	root := fixtureRepo(t)
	if err := saveRun(runRecord{Path: root, Port: 8083, URL: "http://127.0.0.1:8083", PID: 1 << 22}); err != nil {
		t.Fatal(err)
	}
	if err := stopProject(root); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, found, err := loadRun(root); err != nil || found {
		t.Fatalf("stale record was kept: found=%v err=%v", found, err)
	}
}

func TestStopWithNoRecordIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	if err := stopProject(fixtureRepo(t)); err != nil {
		t.Fatalf("stopping a project with no instance failed: %v", err)
	}
}

// fileMode reads the mode of the record written for root.
func fileMode(t *testing.T, root string) os.FileMode {
	t.Helper()
	path, err := runPath(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// fixtureRepo makes a temporary git repository and returns its resolved root.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// alive is the check that keeps a reused pid from being signalled by mistake.
func TestAlive(t *testing.T) {
	if !alive(os.Getpid()) {
		t.Error("the test process reports itself dead")
	}
	if alive(0) || alive(-1) {
		t.Error("a non-positive pid was treated as alive")
	}
	// A pid this large is not in use on any supported system.
	if alive(1 << 22) {
		t.Error("an unused pid was treated as alive")
	}
	if syscall.Kill(os.Getpid(), 0) != nil {
		t.Error("the test process cannot signal itself")
	}
}
