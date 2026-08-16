//go:build !windows

package main

// Nothing double clicks a binary on Unix, so a terminal is always somebody
// else's and closing it is not our business.
func ownsConsole() bool { return false }

func pause() {}
