package single

import (
	"syscall"
	"unsafe"
)

// A named mutex, which Windows releases when the process holding it dies. That
// death-release is the whole point: a lock file left behind by a crash would
// keep every future launch out, and a mutex cannot.
var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
)

const errorAlreadyExists = 183

// Acquire reports whether this process is the first. The handle is deliberately
// never closed: it lives as long as the process and Windows reclaims it.
func Acquire(name string) (bool, error) {
	p, err := syscall.UTF16PtrFromString(`Local\` + name)
	if err != nil {
		return true, err
	}

	_, _, callErr := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(p)))
	if errno, ok := callErr.(syscall.Errno); ok && uintptr(errno) == errorAlreadyExists {
		return false, nil
	}

	return true, nil
}
