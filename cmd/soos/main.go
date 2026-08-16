package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	gosync "sync"
	"syscall"
	"time"

	"github.com/northwest-falls/soos/internal/api"
	"github.com/northwest-falls/soos/internal/browser"
	"github.com/northwest-falls/soos/internal/config"
	"github.com/northwest-falls/soos/internal/index"
	"github.com/northwest-falls/soos/internal/scan"
	"github.com/northwest-falls/soos/internal/sync"
	"github.com/northwest-falls/soos/internal/ui"
	"github.com/northwest-falls/soos/internal/watch"
)

var version = "dev"

const usage = `soos: watches a folder and puts what lands there in your vault.

  soos pair            pair this computer with your account
  soos add <folder>    watch a folder
  soos list            show what is watched, and whether it is paired
  soos share <file>    make sure a file is in your vault, then open it there
  soos once            run one pass and exit
  soos run             keep running
  soos tray            keep running, with an icon in the tray
  soos install         put Soos in place and start him with your computer
  soos uninstall       take him back out

  soos forget          remove this computer's credential

  soos install-menu    add Soos to the right click menu
  soos uninstall-menu  take it back out

  soos --version
`

func main() {
	// Explorer gives us a console of our own and takes it away the instant we
	// return, so the command list is unreadable there. Somebody who arrived by
	// double click gets set up instead of a flash of text.
	if len(os.Args) < 2 {
		if !ownsConsole() {
			fmt.Print(usage)
			os.Exit(2)
		}
		if err := cmdWelcome(); err != nil {
			fmt.Fprintln(os.Stderr, "\n  soos:", err)
		}
		pause()
		return
	}

	var err error

	switch os.Args[1] {
	case "--version", "-version", "version":
		fmt.Printf("soos %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return

	case "--wisdom":
		fmt.Println("My wisdom is both a blessing, and a curse.")
		return

	case "pair":
		err = cmdPair()
	case "add":
		err = cmdAdd(os.Args[2:])
	case "list":
		err = cmdList()
	case "share":
		err = cmdShare(os.Args[2:])
	case "once":
		err = cmdRun(false)
	case "run":
		err = cmdRun(true)
	case "tray":
		err = cmdTray()
	case "install":
		err = cmdInstall()
	case "uninstall":
		err = cmdUninstall()
	case "forget":
		err = cmdForget()
	case "install-menu":
		err = cmdInstallMenu()
	case "uninstall-menu":
		err = cmdUninstallMenu()
	default:
		// A folder dropped onto the program is a request to watch it. The path
		// arrives exactly where a command would, so it has to be ruled out
		// before treating the argument as a mistake.
		if st, e := os.Stat(os.Args[1]); e == nil && st.IsDir() {
			err = cmdAdd(os.Args[1:2])
			break
		}
		fmt.Print(usage)
		if ownsConsole() {
			pause()
		}
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "soos:", err)
		if ownsConsole() {
			pause()
		}
		os.Exit(1)
	}

	if ownsConsole() {
		pause()
	}
}

var browserOpen = browser.Open
var nowFunc = time.Now

func client() (*api.Client, *config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}

	c := api.New(cfg.BaseURL, version)

	token, err := config.LoadToken()
	if err != nil {
		return nil, nil, err
	}
	c.Token = token

	return c, cfg, nil
}

func cmdPair() error {
	c, cfg, err := client()
	if err != nil {
		return err
	}

	host, _ := os.Hostname()
	if host == "" {
		host = "This computer"
	}

	ctx, cancel := signalContext()
	defer cancel()

	start, err := c.PairStart(ctx, host, runtime.GOOS)
	if err != nil {
		return err
	}

	url := start.ApproveURL
	if url == "" {
		url = "https://me.northwestfalls.com/#settings/devices"
	}

	fmt.Printf("\n  Your code is  %s\n\n", start.UserCode)
	fmt.Printf("  Approve it at %s\n", url)
	fmt.Println("  Waiting. Ctrl-C to stop.")

	_ = browser.Open(url)

	res, err := c.PairWait(ctx, start.PollSecret)
	if err != nil {
		return err
	}

	switch res.Status {
	case api.PairExpired:
		return errors.New("that code expired. Run soos pair again")
	case api.PairApproved:
	default:
		return fmt.Errorf("unexpected pairing status %q", res.Status)
	}

	if err := config.SaveToken(res.Token); err != nil {
		return err
	}

	cfg.DeviceID = res.DeviceID
	if err := config.Save(cfg); err != nil {
		return err
	}

	fmt.Println("\n  Paired. Add a folder with: soos add <folder>")

	return nil
}

func cmdAdd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: soos add <folder>")
	}

	abs, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}

	st, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return errors.New("that is a file. Point soos at the folder it is in")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	for _, f := range cfg.Folders {
		if f.Path == abs {
			fmt.Println("  Already watching that one.")
			return nil
		}
	}

	cfg.Folders = append(cfg.Folders, config.Folder{Path: abs})
	if err := config.Save(cfg); err != nil {
		return err
	}

	fmt.Printf("  Watching %s\n", abs)

	return nil
}

func cmdList() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	token, err := config.LoadToken()
	if err != nil {
		return err
	}

	if token == "" {
		fmt.Println("  Not paired. Run: soos pair")
	} else {
		fmt.Println("  Paired.")
	}

	if len(cfg.Folders) == 0 {
		fmt.Println("  No folders yet. Add one with: soos add <folder>")
		return nil
	}

	for _, f := range cfg.Folders {
		extra := ""
		if f.Projects {
			extra += " +sessions"
		}
		if f.Artwork {
			extra += " +artwork"
		}
		fmt.Printf("  %s%s\n", f.Path, extra)
	}

	return nil
}

func ForgetTokenQuiet() error { return config.ForgetToken() }

func cmdForget() error {
	if err := config.ForgetToken(); err != nil {
		return err
	}
	fmt.Println("  Credential removed. This computer is no longer paired.")
	return nil
}

func cmdRun(keepGoing bool) error {
	c, cfg, err := client()
	if err != nil {
		return err
	}

	if c.Token == "" {
		return errors.New("not paired. Run: soos pair")
	}
	if len(cfg.Folders) == 0 {
		return errors.New("no folders yet. Add one with: soos add <folder>")
	}

	idxPath, err := config.IndexPath()
	if err != nil {
		return err
	}

	ix, recovered, err := index.Open(idxPath)
	if err != nil {
		return err
	}
	if recovered {

		fmt.Println("  The local index was unreadable, so it is being rebuilt.")
	}

	ctx, cancel := signalContext()
	defer cancel()

	defer func() {
		if ix.Dirty() {
			if err := ix.Save(); err != nil {
				fmt.Fprintln(os.Stderr, "soos: could not save the index:", err)
			}
		}
	}()

	interval := sync.MinInterval

	for {

		fastest := sync.MaxInterval
		for _, folder := range cfg.Folders {
			if ctx.Err() != nil {
				return nil
			}

			s := &sync.Syncer{
				Root:    folder.Path,
				Opts:    scan.Options{Projects: folder.Projects, Artwork: folder.Artwork},
				Index:   ix,
				Settler: watch.NewSettler(),
				API:     c,
				List:    listFolder,
			}

			out, err := s.Once(ctx)
			if out != nil {
				report(folder.Path, out)
			}

			if ix.Dirty() {
				if err := ix.Save(); err != nil {
					fmt.Fprintln(os.Stderr, "soos: could not save the index:", err)
				}
			}

			if out != nil && out.Stopped {

				fmt.Fprintln(os.Stderr, "soos:", out.Reason)
				_ = config.ForgetToken()
				return errors.New("this computer is no longer paired. Run: soos pair")
			}

			if err != nil && out != nil && !out.Paused {
				fmt.Fprintln(os.Stderr, "soos:", err)
			}

			if out != nil {
				if d := out.NextCheck(interval); d > 0 && d < fastest {
					fastest = d
				}
			}
		}

		if !keepGoing {
			return nil
		}

		interval = fastest

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func report(root string, o *sync.Outcome) {
	if o.Paused {
		fmt.Printf("  %s: paused. %v\n", filepath.Base(root), o.Reason)
		return
	}

	if o.Uploaded == 0 && o.Deduped == 0 && o.Failed == 0 && o.Waiting == 0 {
		return
	}

	fmt.Printf("  %s: %d uploaded, %d already there, %d waiting, %d failed\n",
		filepath.Base(root), o.Uploaded, o.Deduped, o.Waiting, o.Failed)

	for reason, n := range o.Ignored {
		fmt.Printf("    ignored %d: %s\n", n, reason)
	}
}

func listFolder(root string) ([]scan.Entry, error) {
	var out []scan.Entry

	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {

			return nil
		}
		out = append(out, entry{p, fi})
		return nil
	})

	return out, err
}

type entry struct {
	path string
	info os.FileInfo
}

func (e entry) Path() string      { return e.path }
func (e entry) Info() os.FileInfo { return e.info }
func (e entry) IsDir() bool       { return e.info.IsDir() }

var stopTray = make(chan struct{})

var stopOnce gosync.Once

func requestStop() { stopOnce.Do(func() { close(stopTray) }) }

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-ch:
		case <-stopTray:
		}
		cancel()
	}()

	return ctx, cancel
}

const dashboardURL = "https://me.northwestfalls.com"

func cmdTray() error {
	_, cfg, err := client()
	if err != nil {
		return err
	}

	done := make(chan error, 1)

	go func() {
		done <- cmdRun(true)
		ui.Quit()
	}()

	ui.Watching(len(cfg.Folders), 0)

	ui.Run(ui.Options{
		Tooltip: "Soos",
		OnOpen:  func() { _ = browser.Open(dashboardURL) },
		OnQuit:  requestStop,
	})

	return <-done
}
