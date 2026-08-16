package main

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procProcessList = kernel32.NewProc("GetConsoleProcessList")
)

// True when this process is the only one on its console, which means Windows
// made the console for us and will destroy it the moment we return.
//
// That is the difference between a double click in Explorer and a command
// typed into a terminal that was already open. It decides whether output is
// something the person can read or something that vanishes.
func ownsConsole() bool {
	var pids [8]uint32
	n, _, _ := procProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}

func pause() {
	fmt.Print("\n  Press Enter to close. ")
	bufio.NewReader(os.Stdin).ReadString('\n')
}
