//go:build windows

package watch

import (
	"os"
	"syscall"
)

const (
	fileAttributeOffline            = 0x00001000
	fileAttributeRecallOnOpen       = 0x00040000
	fileAttributeRecallOnDataAccess = 0x00400000
)

func IsPlaceholder(path string, _ os.FileInfo) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}

	attrs, err := syscall.GetFileAttributes(p)
	if err != nil {
		return false
	}

	const mask = fileAttributeOffline | fileAttributeRecallOnOpen | fileAttributeRecallOnDataAccess

	return attrs&mask != 0
}

func canOpenExclusive(path string) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}

	h, err := syscall.CreateFile(
		p,
		syscall.GENERIC_READ,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return false
	}

	syscall.CloseHandle(h)

	return true
}
