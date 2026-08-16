package browser

import (
	"os/exec"
	"runtime"
	"strings"
)

func Open(url string) error {
	if !strings.HasPrefix(url, "https://") {

		return errNotHTTPS
	}

	switch runtime.GOOS {
	case "windows":

		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

type openError string

func (e openError) Error() string { return string(e) }

const errNotHTTPS = openError("refusing to open a URL that is not https")
