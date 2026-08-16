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

	if err := install(false); err != nil {
		return err
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

	// Autostart only covers the next sign in. Somebody who has just set this up
	// is entitled to have it working now rather than tomorrow.
	dst, err := installPath()
	if err != nil {
		return err
	}
	if err := startBackground(dst, "tray"); err != nil {
		fmt.Println()
		fmt.Println("  Set up. He will start with your computer.")
		return nil
	}

	fmt.Println()
	fmt.Println("  He is running. Look for the pine cone by the clock.")

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
