//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	verbKey  = `Software\Classes\*\shell\SoosShare`
	verbName = "Send to Northwest Falls"
)

func cmdInstallMenu() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, verbKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if err := k.SetStringValue("", verbName); err != nil {
		return err
	}

	if err := k.SetStringValue("Icon", exe+",0"); err != nil {
		return err
	}

	cmd, _, err := registry.CreateKey(registry.CURRENT_USER, verbKey+`\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer cmd.Close()

	if err := cmd.SetStringValue("", fmt.Sprintf(`"%s" share "%%1"`, exe)); err != nil {
		return err
	}

	fmt.Println("  Added to the right click menu.")
	fmt.Println("  On Windows 11 it is under Show more options.")

	return nil
}

func cmdUninstallMenu() error {

	_ = registry.DeleteKey(registry.CURRENT_USER, verbKey+`\command`)

	if err := registry.DeleteKey(registry.CURRENT_USER, verbKey); err != nil {
		if err == registry.ErrNotExist {
			fmt.Println("  It was not there.")
			return nil
		}
		return err
	}

	fmt.Println("  Removed from the right click menu.")

	return nil
}
