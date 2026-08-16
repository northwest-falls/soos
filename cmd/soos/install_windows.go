//go:build windows

package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runName = "Soos"

func installPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	// UserCacheDir is already %LOCALAPPDATA% on Windows. Taking its parent gave
	// %APPDATA%\Programs, which is not where anything installs, and left setup's
	// copy and this one in different places with the Run key on the wrong one.
	return filepath.Join(base, "Programs", "Soos", "soos.exe"), nil
}

func installAutostart(exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	return k.SetStringValue(runName, `"`+exe+`" tray`)
}

func removeAutostart() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()

	return k.DeleteValue(runName)
}
