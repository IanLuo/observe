// Package serve is the loopback HTTP boundary and composition root
// (.agents/specs/architecture.md D1 and D5): it reads the repository, runs the
// analyzer, keeps the current-state cache, and hands the presenter one immutable
// view per generation. It never invents evidence or upgrades a status.
//
// Access is loopback only. The mutating routes need a per-launch token and a
// same-origin request, so a page the user did not load cannot refresh or clear the
// cache (AC11).
package serve

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"observe/internal/model"
	"observe/internal/repo"
	"observe/internal/store"
)

// Analyzed is one analysis pass, as the presenter needs it.
type Analyzed struct {
	Repository  string
	Snapshot    model.Snapshot
	Baseline    model.SpecBaseline
	Changes     []repo.Change
	Conflicts   []model.PromptConflict
	PromptShown bool
	Cache       Cache
}

// Options configures one server.
type Options struct {
	RepoPath  string
	Prompt    string
	CachePath string // empty disables the cache
	// Analysis is the analysis pass. Nil means the built-in pipeline. Tests supply
	// their own so a failing refresh is reproducible without a broken repository.
	Analysis func(context.Context) (Analyzed, error)
}

// Server serves one repository. State is guarded by one mutex: the product is
// single-user and one repository per instance, so a finer split would not buy
// anything.
type Server struct {
	opts  Options
	token string

	mu        sync.RWMutex
	last      *Analyzed
	freshness string // updating, current, stale, failed
	failure   *Failure
	cache     Cache

	http *http.Server
	url  string
}

// New prepares a server for one repository.
func New(opts Options) (*Server, error) {
	if opts.RepoPath == "" {
		return nil, errors.New("no repository to serve")
	}
	if opts.Analysis == nil {
		opts.Analysis = opts.pipeline
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	s := &Server{opts: opts, token: token, freshness: "updating"}
	s.cache = Cache{}
	return s, nil
}

// Token is the per-launch token the page must send with a mutating request.
func (s *Server) Token() string { return s.token }

// URL is the loopback URL the server listens on, once Listen has been called.
func (s *Server) URL() string { return s.url }

// Refresh runs one analysis pass.
//
// A failure is recorded, not hidden: the last successful snapshot stays visible
// marked stale with the failure time and reason, or the view reports failed when
// nothing has succeeded yet. Retry is the same call (AC9).
func (s *Server) Refresh(ctx context.Context) error {
	s.mu.Lock()
	s.freshness = "updating"
	s.mu.Unlock()

	analyzed, err := s.opts.Analysis(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.failure = &Failure{At: time.Now().UTC().Format(time.RFC3339), Reason: err.Error()}
		if s.last != nil {
			s.freshness = string(model.FreshnessStale)
		} else {
			s.freshness = string(model.FreshnessFailed)
		}
		return err
	}

	analyzed.PromptShown = s.opts.Prompt != ""
	s.last = &analyzed
	s.cache = analyzed.Cache
	s.freshness = string(model.FreshnessCurrent)
	s.failure = nil
	return nil
}

// State is the view the presenter renders: the latest snapshot with its freshness.
func (s *Server) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.last == nil {
		return State{
			Repository:  s.opts.RepoPath,
			Freshness:   s.freshness,
			Failure:     s.failure,
			Baseline:    Baseline{State: "unknown", Specs: []string{}},
			Goals:       []Goal{},
			Conflicts:   []Conflict{},
			Diagnostics: []string{},
			Cache:       s.cache,
		}
	}
	return newState(*s.last, s.freshness, s.failure, s.cache)
}

// Handler is the HTTP contract: one read route, two token-guarded mutations, and
// the page.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/refresh", s.guarded(s.handleRefresh))
	mux.HandleFunc("POST /api/cache/clear", s.guarded(s.handleClear))
	mux.HandleFunc("GET /", s.handlePage)
	return localOnly(mux)
}

// Listen binds a loopback address and serves in the background. An empty address
// means loopback with an OS-assigned port. A non-loopback address is refused: V1
// exposes this machine only (AC11).
func (s *Server) Listen(addr string) (string, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("parse listen address %q: %w", addr, err)
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("refuse non-loopback listen address %q", addr)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("listen on %s: %w", addr, err)
	}

	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	s.url = "http://" + listener.Addr().String()
	go s.http.Serve(listener)
	return s.url, nil
}

// Close stops the HTTP server.
func (s *Server) Close(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

// ClearCache discards the disposable current-state cache. It needs no
// confirmation: the cache is rebuildable and holds no source (AC12).
func (s *Server) ClearCache() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.opts.CachePath == "" {
		s.cache = Cache{State: "disabled"}
		return nil
	}
	handle, err := store.Open(s.opts.CachePath, s.opts.RepoPath)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err := handle.Clear(); err != nil {
		return err
	}
	s.cache = Cache{Path: handle.Path(), State: "absent"}
	return nil
}

// handleState answers the read route.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, true, s.State(), "")
}

// handleRefresh re-runs the analysis. A failed pass still answers with the new
// state, because the state is what explains the failure.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.Refresh(r.Context()); err != nil {
		writeJSON(w, false, s.State(), err.Error())
		return
	}
	writeJSON(w, true, s.State(), "")
}

// handleClear discards the cache and answers with the state that follows.
func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	if err := s.ClearCache(); err != nil {
		writeJSON(w, false, s.State(), err.Error())
		return
	}
	writeJSON(w, true, s.State(), "")
}

// handlePage serves the embedded page with the token it needs for mutations.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Write([]byte(strings.Replace(page, "{{TOKEN}}", s.token, 1)))
}

// guarded checks the token and the origin before a mutation runs. A cross-site
// page can neither read the token nor set the header, and the origin check stops a
// form post from a page the user did not load.
func (s *Server) guarded(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		given := r.Header.Get(tokenHeader)
		if subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) != 1 {
			writeJSON(w, false, s.State(), "refused: missing or wrong "+tokenHeader)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
			writeJSON(w, false, s.State(), "refused: cross-origin request from "+origin)
			return
		}
		switch site := r.Header.Get("Sec-Fetch-Site"); site {
		case "", "same-origin", "none":
		default:
			writeJSON(w, false, s.State(), "refused: request is not same-origin ("+site+")")
			return
		}
		next(w, r)
	}
}

// tokenHeader carries the per-launch token.
const tokenHeader = "X-Observe-Token"

// localOnly refuses a request whose Host is not a loopback name, so a DNS name
// that resolves to 127.0.0.1 cannot reach the API through a browser (AC11).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !isLoopbackHost(host) {
			writeJSON(w, false, nil, "refused: host "+host+" is not loopback")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether origin names the same scheme and host as host.
func sameOrigin(origin, host string) bool {
	stripped := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
	return stripped == host
}

// isLoopbackHost reports whether host names this machine's loopback interface.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// writeJSON answers in the envelope every client reads: ok, data, error.
func writeJSON(w http.ResponseWriter, ok bool, data any, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	envelope := map[string]any{"ok": ok}
	if data != nil {
		envelope["data"] = data
	}
	if message != "" {
		envelope["error"] = message
	}
	if err := json.NewEncoder(w).Encode(envelope); err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
	}
}

// newToken makes an unguessable per-launch token.
func newToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate access token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
