//go:build !windows

package main

import (
	"fmt"
	"os"
)

// Nothing is launched by double click here, so a terminal is always present
// and always somebody else's.
func attachConsole() bool { return true }

func alert(title, body string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, body)
}
