package ui

import (
	_ "embed"
	"runtime"
)

var iconICO []byte

var trayPNG []byte

var iconPNG []byte

func TrayIcon() []byte {
	if runtime.GOOS == "windows" {
		return iconICO
	}
	return trayPNG
}

func AppIcon() []byte { return iconPNG }
