//go:build !windows

package pick

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// Whatever the desktop already has. There is no folder dialog to call on Unix
// without linking a toolkit, and linking one to ask a single question would
// cost more than the rest of the program.
func Folder(title string) (string, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("/usr/bin/osascript", "-e",
			`POSIX path of (choose folder with prompt "`+strings.ReplaceAll(title, `"`, "")+`")`).Output()
		if err != nil {
			// Cancelling is an error to osascript and an answer to us.
			return "", nil
		}
		return strings.TrimSpace(string(out)), nil
	}

	for _, c := range [][]string{
		{"zenity", "--file-selection", "--directory", "--title=" + title},
		{"kdialog", "--getexistingdirectory", "."},
	} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		out, err := exec.Command(c[0], c[1:]...).Output()
		if err != nil {
			return "", nil
		}
		return strings.TrimSpace(string(out)), nil
	}

	return "", errors.New("no folder picker here. Install zenity, or type the path")
}
