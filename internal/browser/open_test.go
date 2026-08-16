package browser

import "testing"

func TestLoopbackAllowed(t *testing.T) {
	for _, u := range []string{
		"http://127.0.0.1:53266/?t=abc",
		"http://localhost:8080/",
		"http://[::1]:9000/?t=x",
	} {
		if !isLoopback(u) {
			t.Errorf("should allow loopback: %s", u)
		}
	}
}

func TestNonLoopbackHTTPRejected(t *testing.T) {
	for _, u := range []string{
		"http://evil.com/",
		"http://127.0.0.1.evil.com/",
		"http://example.com:80/?t=x",
	} {
		if isLoopback(u) {
			t.Errorf("must NOT treat as loopback: %s", u)
		}
	}
}
