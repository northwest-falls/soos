//go:build !windows

package single

import (
	"os"
	"path/filepath"
	"syscall"
)

// An advisory lock on a file the kernel releases when the process dies, for the
// same reason as the Windows mutex: a crash must not lock out the next launch.
// The file descriptor is held open for the life of the process on purpose.
func Acquire(name string) (bool, error) {
	path := filepath.Join(os.TempDir(), name+".lock")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true, err
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return false, nil
	}

	return true, nil
}
