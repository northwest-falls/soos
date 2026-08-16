package main

import (
	"os"
	"syscall"
	"unsafe"
)

// Soos is built for the GUI subsystem, so Windows never makes a console for
// him and opening him from Explorer shows no black window at all.
//
// The cost is that a terminal gets no output either, unless we ask for the one
// that is already there. AttachConsole against the parent does that: run from a
// prompt it succeeds and everything prints as usual, opened from Explorer it
// fails and there was never a window to flash.
//
// The alternative, hiding a console after creating it, is what malware does and
// scanners know it.
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	procAttachConsole = kernel32.NewProc("AttachConsole")
	procMessageBox    = user32.NewProc("MessageBoxW")
)

const attachParent = ^uintptr(0) // (DWORD)-1

func attachConsole() bool {
	if r, _, _ := procAttachConsole.Call(attachParent); r == 0 {
		return false
	}

	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	os.Stdout = out
	os.Stderr = out

	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = in
	}

	return true
}

// With no console there is nowhere for an error to go, and silence after a
// double click is indistinguishable from being deleted by antivirus.
func alert(title, body string) {
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	b, err := syscall.UTF16PtrFromString(body)
	if err != nil {
		return
	}
	procMessageBox.Call(0, uintptr(unsafe.Pointer(b)), uintptr(unsafe.Pointer(t)), 0x40)
}
