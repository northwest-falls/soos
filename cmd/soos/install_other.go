//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

func installPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Applications", "Soos.app", "Contents", "MacOS", "soos"), nil
	}
	return filepath.Join(home, ".local", "bin", "soos"), nil
}

func installAutostart(exe string) error {
	if runtime.GOOS == "darwin" {
		return errors.New("add Soos under System Settings, Login Items")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dir := filepath.Join(home, ".config", "autostart")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	entry := "[Desktop Entry]\nType=Application\nName=Soos\nExec=" + exe + " tray\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"

	return os.WriteFile(filepath.Join(dir, "soos.desktop"), []byte(entry), 0o644)
}

func removeAutostart() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(home, ".config", "autostart", "soos.desktop"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
