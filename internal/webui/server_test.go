package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer() *server {
	return &server{
		deps:  Deps{Version: "test", Paired: func() bool { return false }},
		token: "secret-token",
		addr:  "127.0.0.1:9999",
	}
}

// The whole security model is: loopback plus a token. A page on the internet
// can send requests to 127.0.0.1, so both halves have to hold on their own.
func TestGuardRejectsWithoutToken(t *testing.T) {
	s := newTestServer()
	h := s.guard(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	r := httptest.NewRequest("GET", "http://127.0.0.1:9999/api/state", nil)
	r.Host = "127.0.0.1:9999"
	w := httptest.NewRecorder()
	h(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("no token should be forbidden, got %d", w.Code)
	}
}

func TestGuardRejectsWrongToken(t *testing.T) {
	s := newTestServer()
	h := s.guard(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	r := httptest.NewRequest("GET", "http://127.0.0.1:9999/api/state", nil)
	r.Host = "127.0.0.1:9999"
	r.Header.Set("X-Soos-Token", "wrong")
	w := httptest.NewRecorder()
	h(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("wrong token should be forbidden, got %d", w.Code)
	}
}

// A DNS name pointed at 127.0.0.1 arrives with a token if the attacker read one
// request, so the Host has to be the loopback socket by number as well.
func TestGuardRejectsForeignHost(t *testing.T) {
	s := newTestServer()
	h := s.guard(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	r := httptest.NewRequest("GET", "http://evil.example.com/api/state", nil)
	r.Host = "evil.example.com"
	r.Header.Set("X-Soos-Token", "secret-token")
	w := httptest.NewRecorder()
	h(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign host should be forbidden even with a good token, got %d", w.Code)
	}
}

func TestGuardAllowsCorrect(t *testing.T) {
	s := newTestServer()
	called := false
	h := s.guard(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(200) })

	r := httptest.NewRequest("GET", "http://127.0.0.1:9999/api/state", nil)
	r.Host = "127.0.0.1:9999"
	r.Header.Set("X-Soos-Token", "secret-token")
	w := httptest.NewRecorder()
	h(w, r)

	if w.Code != 200 || !called {
		t.Fatalf("correct request should pass, got %d called=%v", w.Code, called)
	}
}

// The page carries the token in its markup, so a leak there is a leak of
// everything. It must never be served to a request that has not already proven
// it holds the token.
func TestPageNeedsToken(t *testing.T) {
	s := newTestServer()
	r := httptest.NewRequest("GET", "http://127.0.0.1:9999/", nil)
	r.Host = "127.0.0.1:9999"
	w := httptest.NewRecorder()
	s.handlePage(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("page without token should be forbidden, got %d", w.Code)
	}
}
