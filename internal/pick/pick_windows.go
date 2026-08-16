package pick

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

// The folder dialog, called directly rather than through PowerShell.
//
// Shelling out would be four lines instead of eighty. It would also mean a
// program that reads your files and uploads them spawns a script host, which is
// near the top of every behavioural detection list there is. Soos has already
// been deleted as a trojan once. Eighty lines is cheap.
var (
	shell32 = syscall.NewLazyDLL("shell32.dll")
	ole32   = syscall.NewLazyDLL("ole32.dll")

	procBrowseForFolder = shell32.NewProc("SHBrowseForFolderW")
	procPathFromIDList  = shell32.NewProc("SHGetPathFromIDListW")
	procCoInitializeEx  = ole32.NewProc("CoInitializeEx")
	procCoUninitialize  = ole32.NewProc("CoUninitialize")
	procCoTaskMemFree   = ole32.NewProc("CoTaskMemFree")
)

type browseInfo struct {
	owner       uintptr
	root        uintptr
	displayName *uint16
	title       *uint16
	flags       uint32
	fn          uintptr
	lparam      uintptr
	image       int32
}

const (
	returnOnlyDirs = 0x0001
	newDialogStyle = 0x0040
	apartment      = 0x2
	maxPath        = 260
)

// Folder opens the picker. An empty string with no error means cancelled,
// which is an ordinary answer rather than a failure.
func Folder(title string) (string, error) {
	// The dialog is apartment threaded and the new style needs COM, so this
	// goroutine has to stay on the thread that initialised it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	procCoInitializeEx.Call(0, apartment)
	defer procCoUninitialize.Call()

	heading, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return "", err
	}

	display := make([]uint16, maxPath)

	bi := browseInfo{
		displayName: &display[0],
		title:       heading,
		flags:       returnOnlyDirs | newDialogStyle,
	}

	pidl, _, _ := procBrowseForFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", nil
	}
	defer procCoTaskMemFree.Call(pidl)

	buf := make([]uint16, maxPath)
	ok, _, _ := procPathFromIDList.Call(pidl, uintptr(unsafe.Pointer(&buf[0])))
	if ok == 0 {
		return "", errors.New("that is not a folder on disk")
	}

	return syscall.UTF16ToString(buf), nil
}
