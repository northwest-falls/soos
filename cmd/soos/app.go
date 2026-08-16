package main

import (
	"context"
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

	host := &uiHost{}

	// The page is opened for setup and the occasional look, not kept up. So the
	// server starts when it is wanted and closes its socket when it has been
	// idle a while, and the steady state of the tray is a process that only
	// watches a folder and uploads, with nothing listening. That is a plainer
	// thing for a scanner to look at.
	if !isPaired() || !watching() {
		host.open(ctx)
	}

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
		OnOpen:  func() { host.open(ctx) },
		OnQuit:  requestStop,
	})

	return nil
}

// uiHost runs the interface only while it is wanted. The first request starts
// it, idleness stops it, and asking again starts it back up.
type uiHost struct {
	mu  sync.Mutex
	url string
}

func (h *uiHost) open(parent context.Context) {
	h.mu.Lock()
	if h.url != "" {
		u := h.url
		h.mu.Unlock()
		_ = browser.Open(u)
		return
	}

	ctx, cancel := context.WithCancel(parent)
	ready := make(chan string, 1)
	h.mu.Unlock()

	go func() {
		err := webui.Serve(ctx, webui.Deps{
			Version:      version,
			Paired:       isPaired,
			StartPair:    startPair,
			IdleShutdown: 10 * time.Minute,
		}, func(u string) error {
			h.mu.Lock()
			h.url = u
			h.mu.Unlock()
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

		// Stopped, whether idle or shutting down. Forget the address so the next
		// open starts a fresh one rather than pointing at a dead port.
		h.mu.Lock()
		h.url = ""
		h.mu.Unlock()
		clearUIAddr()
		cancel()
	}()

	select {
	case u := <-ready:
		_ = browser.Open(u)
	case <-time.After(3 * time.Second):
	}
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
