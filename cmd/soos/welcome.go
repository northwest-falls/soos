package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/northwest-falls/soos/internal/config"
)

// What happens when somebody downloads Soos and double clicks him.
//
// The command list is the right answer for a terminal and the wrong answer for
// Explorer, where it appears for a fraction of a second and takes the window
// with it. Anyone who reaches this has not been told to type anything, so this
// asks for the two things only they can supply and does the rest.
func cmdWelcome() error {
	fmt.Println()
	fmt.Println("  Soos")
	fmt.Println("  He watches a folder and puts what lands there in your vault.")
	fmt.Println()

	// Setup has already put him here and written the registry entries, so doing
	// it again would be the self-copy this was moved out of Soos to avoid.
	// Started from a download folder instead, there is no setup to have done
	// it, and he still has to put himself somewhere.
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)

	dst, err := installPath()
	if err != nil {
		return err
	}

	if !strings.EqualFold(self, dst) {
		if err := install(false); err != nil {
			return err
		}
	}

	token, err := config.LoadToken()
	if err != nil {
		return err
	}
	if token == "" {
		fmt.Println()
		if err := cmdPair(); err != nil {
			return err
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if len(cfg.Folders) == 0 {
		if err := askForFolder(cfg); err != nil {
			return err
		}
	}

	if len(cfg.Folders) == 0 {
		fmt.Println()
		fmt.Println("  Nothing to watch yet. Open me again whenever you have a folder.")
		return nil
	}

	// One pass here, in the window that is already open, rather than launching
	// a copy of himself in the background.
	//
	// A program that copies itself into AppData, writes a Run key and then
	// spawns a hidden instance has done the three things a dropper does, in the
	// order a dropper does them. Antivirus is right to score that, and no
	// amount of being innocent gets the sequence past it.
	//
	// It is also just better. The first thing anyone sees is their own music
	// going up, in front of them, rather than a promise.
	fmt.Println()
	fmt.Println("  Taking a first look now.")

	if err := cmdRun(false); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("  Done. He starts with your computer from now on.")

	return nil
}

// Dragging a folder into a console window pastes its path, which is the only
// way to pick a folder here that does not need a window toolkit shipped with
// it. Typing it out still works for anyone who would rather.
func askForFolder(cfg *config.Config) error {
	fmt.Println()
	fmt.Println("  Last thing: which folder should he watch?")
	fmt.Println("  Drag it into this window and press Enter, or press Enter to skip.")
	fmt.Print("\n  > ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return nil
	}

	// Explorer quotes anything containing a space.
	path := strings.TrimSpace(line)
	path = strings.Trim(path, `"'`)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	st, err := os.Stat(abs)
	if err != nil {
		fmt.Println("\n  Could not find that folder. Add it later with: soos add <folder>")
		return nil
	}
	if !st.IsDir() {
		// Dragging the track rather than the folder holding it is the obvious
		// mistake, and the folder above it is almost certainly what was meant.
		abs = filepath.Dir(abs)
	}

	cfg.Folders = append(cfg.Folders, config.Folder{Path: abs})
	if err := config.Save(cfg); err != nil {
		return err
	}

	fmt.Printf("\n  Watching %s\n", abs)

	return nil
}
