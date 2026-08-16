package webui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/northwest-falls/soos/internal/config"
	"github.com/northwest-falls/soos/internal/pick"
)

//go:embed page.html
var page string

type Deps struct {
	Version string

	// Begins pairing and finishes it in the background. The page watches
	// Paired rather than holding a request open for ten minutes.
	StartPair func(ctx context.Context) (code string, approveURL string, err error)
	Paired    func() bool

	// After this long with no request, the server shuts itself down and the
	// listening socket goes away. Zero keeps it up for as long as it is asked
	// to run. A freshly installed program that keeps a socket open forever is
	// the shape of a backdoor, and there is no reason to be that shape when
	// nobody is looking at the page.
	IdleShutdown time.Duration
}

type server struct {
	deps  Deps
	token string
	addr  string

	lastReq atomic.Int64
}

// Serve opens the interface and returns when the context is cancelled.
//
// Bound to the loopback address on a port the operating system picks, and every
// request carries a token minted at startup. A page on the internet can reach
// 127.0.0.1, so the token is what stops any site you happen to have open from
// reading which folders you watch or adding one of its own.
func Serve(ctx context.Context, deps Deps, open func(string) error) error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	s := &server{deps: deps, token: hex.EncodeToString(raw), addr: ln.Addr().String()}

	mux := http.NewServeMux()
	// Browsers ask for this unprompted, and with no handler it is a 404 that
	// the CSP then complains about in the console. An empty 204 is quieter than
	// carrying an icon that has to be embedded and kept in sync.
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", s.handlePage)
	mux.HandleFunc("/api/state", s.guard(s.handleState))
	mux.HandleFunc("/api/pick", s.guard(s.handlePick))
	mux.HandleFunc("/api/folders/add", s.guard(s.handleAdd))
	mux.HandleFunc("/api/folders/remove", s.guard(s.handleRemove))
	mux.HandleFunc("/api/folders/options", s.guard(s.handleOptions))
	mux.HandleFunc("/api/pair", s.guard(s.handlePair))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	url := fmt.Sprintf("http://%s/?t=%s", s.addr, s.token)
	if open != nil {
		_ = open(url)
	}
	fmt.Println("  Soos is open at", url)

	s.lastReq.Store(time.Now().UnixNano())

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	if deps.IdleShutdown > 0 {
		go func() {
			// Check a few times within the window rather than on a fixed clock,
			// so a short window is honoured about as promptly as a long one.
			step := deps.IdleShutdown / 3
			if step > 30*time.Second {
				step = 30 * time.Second
			}
			if step < 50*time.Millisecond {
				step = 50 * time.Millisecond
			}
			t := time.NewTicker(step)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					idle := time.Since(time.Unix(0, s.lastReq.Load()))
					if idle >= deps.IdleShutdown {
						shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
						_ = srv.Shutdown(shutdown)
						cancel()
						return
					}
				}
			}
		}()
	}

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// A name in DNS can be pointed at 127.0.0.1, so a matching token is necessary
// but not sufficient. Anything not addressed to the loopback socket by number
// was not addressed to us.
func (s *server) sameOrigin(r *http.Request) bool {
	return r.Host == s.addr
}

func (s *server) authed(r *http.Request) bool {
	given := r.Header.Get("X-Soos-Token")
	if given == "" {
		given = r.URL.Query().Get("t")
	}
	return subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) == 1
}

func (s *server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.sameOrigin(r) || !s.authed(r) {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		s.lastReq.Store(time.Now().UnixNano())
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.sameOrigin(r) || !s.authed(r) {
		http.Error(w, "Open Soos from the tray icon.", http.StatusForbidden)
		return
	}

	s.lastReq.Store(time.Now().UnixNano())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'")

	fmt.Fprint(w, strings.ReplaceAll(page, "__TOKEN__", s.token))
}

type folderView struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Projects bool   `json:"projects"`
	Artwork  bool   `json:"artwork"`
	Missing  bool   `json:"missing"`
}

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		fail(w, err)
		return
	}

	out := make([]folderView, 0, len(cfg.Folders))
	for _, f := range cfg.Folders {
		st, err := os.Stat(f.Path)
		out = append(out, folderView{
			Path:     f.Path,
			Name:     filepath.Base(f.Path),
			Projects: f.Projects,
			Artwork:  f.Artwork,
			Missing:  err != nil || !st.IsDir(),
		})
	}

	send(w, map[string]any{
		"version": s.deps.Version,
		"paired":  s.deps.Paired(),
		"folders": out,
	})
}

func (s *server) handlePick(w http.ResponseWriter, r *http.Request) {
	path, err := pick.Folder("Which folder should Soos watch?")
	if err != nil {
		fail(w, err)
		return
	}
	send(w, map[string]any{"path": path})
}

func (s *server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var body struct{ Path string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, err)
		return
	}

	abs, err := filepath.Abs(strings.TrimSpace(body.Path))
	if err != nil {
		fail(w, err)
		return
	}

	st, err := os.Stat(abs)
	if err != nil {
		fail(w, fmt.Errorf("could not find that folder"))
		return
	}
	if !st.IsDir() {
		abs = filepath.Dir(abs)
	}

	cfg, err := config.Load()
	if err != nil {
		fail(w, err)
		return
	}

	for _, f := range cfg.Folders {
		if strings.EqualFold(f.Path, abs) {
			send(w, map[string]any{"ok": true})
			return
		}
	}

	cfg.Folders = append(cfg.Folders, config.Folder{Path: abs})
	if err := config.Save(cfg); err != nil {
		fail(w, err)
		return
	}

	send(w, map[string]any{"ok": true})
}

func (s *server) handleRemove(w http.ResponseWriter, r *http.Request) {
	var body struct{ Path string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, err)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fail(w, err)
		return
	}

	kept := cfg.Folders[:0]
	for _, f := range cfg.Folders {
		if !strings.EqualFold(f.Path, body.Path) {
			kept = append(kept, f)
		}
	}
	cfg.Folders = kept

	if err := config.Save(cfg); err != nil {
		fail(w, err)
		return
	}

	send(w, map[string]any{"ok": true})
}

func (s *server) handleOptions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path     string
		Projects bool
		Artwork  bool
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, err)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fail(w, err)
		return
	}

	for i := range cfg.Folders {
		if strings.EqualFold(cfg.Folders[i].Path, body.Path) {
			cfg.Folders[i].Projects = body.Projects
			cfg.Folders[i].Artwork = body.Artwork
		}
	}

	if err := config.Save(cfg); err != nil {
		fail(w, err)
		return
	}

	send(w, map[string]any{"ok": true})
}

func (s *server) handlePair(w http.ResponseWriter, r *http.Request) {
	code, url, err := s.deps.StartPair(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	send(w, map[string]any{"code": code, "url": url})
}

func send(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
}
