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
	// %LOCALAPPDATA%\Programs, the per-user location. No administrator prompt,
	// which is the thing people cancel during setup.
	return filepath.Join(filepath.Dir(base), "Programs", "Soos", "soos.exe"), nil
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
