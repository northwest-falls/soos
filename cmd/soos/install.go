package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Soos installs himself rather than shipping a separate installer.
//
// One binary that can put itself in place is smaller, needs no packaging
// toolchain, and can be read by the person running it. A second program whose
// only job is to copy the first is a second program to sign, ship and trust.
func cmdInstall() error { return install(true) }

func install(showNext bool) error {
	src, err := os.Executable()
	if err != nil {
		return err
	}
	src, _ = filepath.EvalSymlinks(src)

	dst, err := installPath()
	if err != nil {
		return err
	}

	if strings.EqualFold(src, dst) {
		fmt.Println("  Already installed here.")
	} else {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyExe(src, dst); err != nil {
			return fmt.Errorf("could not put Soos in place: %w", err)
		}
		fmt.Printf("  Installed to %s\n", dst)
	}

	if err := installAutostart(dst); err != nil {
		fmt.Println("  Could not set him to start with your computer:", err)
	} else {
		fmt.Println("  He will start with your computer.")
	}

	if err := installMenuFor(dst); err != nil {
		fmt.Println("  Could not add the right click entry:", err)
	}

	if showNext {
		fmt.Println()
		fmt.Println("  Next: soos pair")
	}

	return nil
}

func cmdUninstall() error {
	dst, err := installPath()
	if err != nil {
		return err
	}

	_ = removeAutostart()
	_ = cmdUninstallMenu()

	// The credential goes too. Leaving a working key behind on a machine
	// somebody just cleaned off is not tidy, it is careless.
	if err := ForgetTokenQuiet(); err != nil {
		fmt.Println("  Could not remove the credential:", err)
	}

	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)

	if strings.EqualFold(self, dst) {
		// A running binary cannot delete itself on Windows, and doing it by
		// hand is one line. Saying so beats a silent failure.
		fmt.Println("  Everything else is removed.")
		fmt.Printf("  Delete the program itself when you like: %s\n", dst)
		return nil
	}

	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	fmt.Println("  Removed. Your files were not touched.")

	return nil
}

func copyExe(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Written beside the target and renamed, so a half-copied binary is never
	// left where the autostart entry points.
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	// Windows refuses to rename over a running executable, so the old one is
	// moved aside first and cleaned up on the next install.
	if runtime.GOOS == "windows" {
		_ = os.Remove(dst + ".old")
		_ = os.Rename(dst, dst+".old")
	}

	return os.Rename(tmp, dst)
}
