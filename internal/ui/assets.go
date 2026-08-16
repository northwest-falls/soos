package ui

import (
	_ "embed"
	"runtime"
)

// These are go:embed directives, not comments. A pass that shortens comments
// must leave them alone: without them the variables are empty and the tray
// icon, and the icon on the exe, are blank.

//go:embed icon.ico
var iconICO []byte

//go:embed tray.png
var trayPNG []byte

//go:embed icon.png
var iconPNG []byte

func TrayIcon() []byte {
	if runtime.GOOS == "windows" {
		return iconICO
	}
	return trayPNG
}

func AppIcon() []byte { return iconPNG }
