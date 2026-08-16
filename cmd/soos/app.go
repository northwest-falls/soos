package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/northwest-falls/soos/internal/api"
	"github.com/northwest-falls/soos/internal/browser"
	"github.com/northwest-falls/soos/internal/config"
	"github.com/northwest-falls/soos/internal/ui"
	"github.com/northwest-falls/soos/internal/webui"
)

// Soos with nobody typing at him: the tray, the folder watcher, and a page to
// change things on. This is what setup starts and what the Run key starts.
func runApp() error {
	ctx, cancel := signalContext()
	defer cancel()

	var page atomic.Value

	go func() {
		err := webui.Serve(ctx, webui.Deps{
			Version:   version,
			Paired:    isPaired,
			StartPair: startPair,
		}, func(u string) error {
			page.Store(u)
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "soos:", err)
		}
	}()

	open := func() {
		if u, ok := page.Load().(string); ok && u != "" {
			_ = browser.Open(u)
		}
	}

	// Nothing to do until somebody has paired and named a folder, and sitting
	// silently in the tray on a fresh install reads as not working.
	if !isPaired() || !watching() {
		for i := 0; i < 100 && page.Load() == nil; i++ {
			time.Sleep(20 * time.Millisecond)
		}
		open()
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
		OnOpen:  open,
		OnQuit:  requestStop,
	})

	return nil
}

// The page on its own, for anyone who would rather not have a tray icon.
func cmdUI() error {
	ctx, cancel := signalContext()
	defer cancel()

	return webui.Serve(ctx, webui.Deps{
		Version:   version,
		Paired:    isPaired,
		StartPair: startPair,
	}, browser.Open)
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
