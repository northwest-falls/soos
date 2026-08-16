//go:build !windows

package main

import (
	"fmt"
	"runtime"
)

func cmdInstallMenu() error {
	fmt.Printf("  Not built for %s yet.\n", runtime.GOOS)
	fmt.Println("  soos share <file> works from a terminal in the meantime.")
	return nil
}

func cmdUninstallMenu() error {
	fmt.Printf("  Nothing to remove on %s.\n", runtime.GOOS)
	return nil
}
