package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/northwest-falls/soos/internal/api"
	"github.com/northwest-falls/soos/internal/browser"
	"github.com/northwest-falls/soos/internal/config"
	"github.com/northwest-falls/soos/internal/index"
	"github.com/northwest-falls/soos/internal/single"
	"github.com/northwest-falls/soos/internal/ui"
	"github.com/northwest-falls/soos/internal/webui"
)

// Soos with nobody typing at him: the tray, the folder watcher, and a page to
// change things on. This is what setup starts and what the Run key starts.
func runApp() error {
	// One Soos per machine. Setup launches him, the Run key launches him, and a
	// person clicking the icon launches him, so without this there would be
	// several trays and several servers on several ports, each orphaning the
	// last one's open page. That last part is exactly the fault that looked
	// like the app doing nothing.
	first, err := single.Acquire("soos-agent")
	if err == nil && !first {
		if u := readUIAddr(); u != "" {
			_ = browser.Open(u)
		}
		return nil
	}

	ctx, cancel := signalContext()
	defer cancel()
	defer clearUIAddr()

	// A separate token from the one the settings page uses. The vault is handed
	// this one and only this one, so it can play a file off disk without being
	// able to touch anything else the server does.
	localToken := randomToken()

	var mu sync.Mutex
	var pageURL string
	setURL := func(u string) { mu.Lock(); pageURL = u; mu.Unlock() }
	getURL := func() string { mu.Lock(); defer mu.Unlock(); return pageURL }

	ready := make(chan string, 1)

	// The server stays up for as long as Soos runs, so the vault can reach the
	// local playback bridge whenever it is open, not only while the settings
	// page happens to be. That is the socket the on-demand server used to close;
	// it is narrow, loopback, and serves only audio the account already holds.
	go func() {
		err := webui.Serve(ctx, webui.Deps{
			Version:    version,
			Paired:     isPaired,
			StartPair:  startPair,
			LocalToken: localToken,
			FindLocal:  findLocalFile,
		}, func(u string) error {
			setURL(u)
			writeUIAddr(u)
			select {
			case ready <- u:
			default:
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "soos:", err)
		}
	}()

	open := func() {
		if u := getURL(); u != "" {
			_ = browser.Open(u)
		}
	}

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
	}

	// Nothing to do until somebody has paired and named a folder, and sitting
	// silently in the tray on a fresh install reads as not working.
	if !isPaired() || !watching() {
		open()
	}

	go registerLoop(ctx, localToken, getURL)

	go func() {
		for ctx.Err() == nil {
			if isPaired() && watching() {
				if err := cmdRun(true); err != nil {
					fmt.Fprintln(os.Stderr, "soos:", err)
				}
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()

	cfg, _ := config.Load()
	if cfg != nil {
		ui.Watching(len(cfg.Folders), 0)
	}

	ui.Run(ui.Options{
		Tooltip: "Soos",
		OnOpen:  open,
		OnQuit:  requestStop,
	})

	return nil
}

// registerLoop keeps the account pointed at this agent's loopback address while
// it runs, renewing before the short server-side TTL lapses so a closed laptop
// drops off on its own.
func registerLoop(ctx context.Context, token string, getURL func() string) {
	wait := 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 90 * time.Second

		if !isPaired() {
			continue
		}
		u := getURL()
		if u == "" {
			continue
		}

		c, _, err := client()
		if err != nil || c.Token == "" {
			continue
		}
		_ = c.LocalRegister(ctx, baseURLOf(u), token)
	}
}

// findLocalFile answers whether this machine still holds the file for a content
// hash, and where. The index is opened fresh each time rather than shared with
// the uploader: a playback lookup is rare, and a stale open cannot corrupt the
// live one. The size and time are checked so a file changed since it was hashed
// is not served as if it were the version the vault knows.
func findLocalFile(hash string) (string, bool) {
	if hash == "" {
		return "", false
	}

	idxPath, err := config.IndexPath()
	if err != nil {
		return "", false
	}

	ix, _, err := index.Open(idxPath)
	if err != nil {
		return "", false
	}

	for _, p := range ix.KnownAt(hash) {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if e, ok := ix.Lookup(p, st.Size(), st.ModTime()); ok && e.Hash == hash {
			return p, true
		}
	}

	return "", false
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// baseURLOf drops the path and query, leaving http://host. The registered
// address carries no token: the vault gets that separately.
func baseURLOf(u string) string {
	const p = "http://"
	if !strings.HasPrefix(u, p) {
		return u
	}
	rest := u[len(p):]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return p + rest[:i]
	}
	return u
}

// The page on its own, for anyone who would rather not have a tray icon.
//
// Shares the single-instance lock with the tray, so running this while Soos is
// already up opens the page he is already serving rather than a second one.
func cmdUI() error {
	first, err := single.Acquire("soos-agent")
	if err == nil && !first {
		if u := readUIAddr(); u != "" {
			return browser.Open(u)
		}
		return nil
	}

	ctx, cancel := signalContext()
	defer cancel()
	defer clearUIAddr()

	return webui.Serve(ctx, webui.Deps{
		Version:      version,
		Paired:       isPaired,
		StartPair:    startPair,
		IdleShutdown: 10 * time.Minute,
	}, func(u string) error {
		writeUIAddr(u)
		return browser.Open(u)
	})
}

func writeUIAddr(url string) {
	p, err := config.UIAddrPath()
	if err != nil {
		return
	}
	// On a fresh install nothing has been saved yet, so the directory the
	// address lives in does not exist. Without this the second launch has no
	// address to open and falls back to doing nothing.
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(url), 0o600)
}

func readUIAddr() string {
	p, err := config.UIAddrPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func clearUIAddr() {
	if p, err := config.UIAddrPath(); err == nil {
		_ = os.Remove(p)
	}
}

func isPaired() bool {
	token, err := config.LoadToken()
	return err == nil && token != ""
}

func watching() bool {
	cfg, err := config.Load()
	return err == nil && len(cfg.Folders) > 0
}

// Returns as soon as there is a code to show. Waiting for approval takes as
// long as somebody takes to walk to their browser, which is not something to
// hold an HTTP request open for.
func startPair(context.Context) (string, string, error) {
	c, cfg, err := client()
	if err != nil {
		return "", "", err
	}

	host, _ := os.Hostname()
	if host == "" {
		host = "This computer"
	}

	start, err := c.PairStart(context.Background(), host, runtime.GOOS)
	if err != nil {
		return "", "", err
	}

	url := start.ApproveURL
	if url == "" {
		url = "https://me.northwestfalls.com/#settings/devices"
	}

	// The code rides along so the account page can fill it in and leave one
	// approve click, rather than a code to read off one screen and type into
	// another. The page still requires that click: it is what proves the person
	// approving owns the account.
	if start.UserCode != "" {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + "code=" + start.UserCode
	}

	go func() {
		res, err := c.PairWait(context.Background(), start.PollSecret)
		if err != nil || res.Status != api.PairApproved {
			return
		}
		if err := config.SaveToken(res.Token); err != nil {
			return
		}
		cfg.DeviceID = res.DeviceID
		_ = config.Save(cfg)
	}()

	_ = browser.Open(url)

	return start.UserCode, url, nil
}
