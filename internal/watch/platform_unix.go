//go:build !windows

package watch

import (
	"os"
	"syscall"
)

func IsPlaceholder(_ string, info os.FileInfo) bool {
	if info == nil {
		return false
	}

	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}

	size := info.Size()
	if size < 1<<20 {

		return false
	}

	allocated := int64(st.Blocks) * 512

	return allocated*8 < size
}

func canOpenExclusive(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}

	f.Close()

	return true
}
