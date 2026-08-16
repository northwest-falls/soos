package ui

import (
	"fmt"
	"sync"

	"fyne.io/systray"
)

type Options struct {
	Tooltip string
	OnOpen  func()
	OnSync  func()
	OnQuit  func()
}

var (
	mu     sync.Mutex
	status *systray.MenuItem
	last   string
)

func Run(opts Options) {
	systray.Run(func() { ready(opts) }, func() {
		if opts.OnQuit != nil {
			opts.OnQuit()
		}
	})
}

func ready(opts Options) {
	systray.SetIcon(TrayIcon())
	systray.SetTitle("")

	tooltip := opts.Tooltip
	if tooltip == "" {
		tooltip = "Soos"
	}
	systray.SetTooltip(tooltip)

	mu.Lock()
	status = systray.AddMenuItem("Starting", "")
	status.Disable()
	if last != "" {
		status.SetTitle(last)
	}
	mu.Unlock()

	systray.AddSeparator()

	open := systray.AddMenuItem("Open my vault", "")
	syncNow := systray.AddMenuItem("Check now", "")

	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit Soos", "")

	go func() {
		for {
			select {
			case <-open.ClickedCh:
				if opts.OnOpen != nil {
					opts.OnOpen()
				}
			case <-syncNow.ClickedCh:
				if opts.OnSync != nil {
					opts.OnSync()
				}
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func SetStatus(text string) {
	mu.Lock()
	defer mu.Unlock()

	last = text
	if status != nil {
		status.SetTitle(text)
	}
}

func Watching(folders, uploaded int) {
	word := "folders"
	if folders == 1 {
		word = "folder"
	}
	if uploaded > 0 {
		SetStatus(fmt.Sprintf("Watching %d %s, %d sent", folders, word, uploaded))
		return
	}
	SetStatus(fmt.Sprintf("Watching %d %s", folders, word))
}

func Quit() { systray.Quit() }
