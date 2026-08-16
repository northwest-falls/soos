//go:build !windows

package main

import "os/exec"

// Nothing double clicks a binary on Unix, so a terminal is always somebody
// else's and closing it is not our business.
func ownsConsole() bool { return false }

func pause() {}

func startBackground(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
