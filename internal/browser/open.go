package browser

import (
	"os/exec"
	"runtime"
	"strings"
)

// Open sends a URL to the default browser.
//
// https for anything on the internet, so a bug that put a plain-http address
// here could not be turned into a downgrade. The one exception is the loopback
// interface Soos serves for himself, which is http by nature and never leaves
// the machine.
func Open(url string) error {
	if !strings.HasPrefix(url, "https://") && !isLoopback(url) {
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

func isLoopback(url string) bool {
	for _, p := range []string{
		"http://127.0.0.1:", "http://127.0.0.1/",
		"http://localhost:", "http://localhost/",
		"http://[::1]:", "http://[::1]/",
	} {
		if strings.HasPrefix(url, p) {
			return true
		}
	}
	return false
}

type openError string

func (e openError) Error() string { return string(e) }

const errNotHTTPS = openError("refusing to open a URL that is not https")
