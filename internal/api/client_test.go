package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := New(srv.URL, "0.0.1-test")
	c.HTTP = srv.Client()

	return c
}

func TestHaveSplitsOversizedBatches(t *testing.T) {
	var calls int
	var sizes []int

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++

		var in struct {
			Hashes []string `json:"hashes"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		sizes = append(sizes, len(in.Hashes))

		if len(in.Hashes) > MaxHashesPerCall {
			t.Errorf("server would refuse a batch of %d", len(in.Hashes))
		}

		var have []string
		for i, h := range in.Hashes {
			if i%2 == 0 {
				have = append(have, h)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"have": have})
	})

	hashes := make([]string, 1200)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("%064x", i)
	}

	got, err := c.Have(context.Background(), hashes)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 3 {
		t.Fatalf("1200 hashes at %d per call should be 3 requests, got %d (%v)", MaxHashesPerCall, calls, sizes)
	}
	if want := 600; len(got) != want {
		t.Fatalf("want %d found, got %d", want, len(got))
	}
	if !got[hashes[0]] || got[hashes[1]] {
		t.Fatal("results from separate batches were not merged correctly")
	}
}

func TestHaveWithNothingMakesNoRequest(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("an empty scan must not call the server")
	})

	got, err := c.Have(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %d", len(got))
	}
}

func TestUnauthorizedIsDistinct(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := c.Have(context.Background(), []string{fmt.Sprintf("%064x", 1)})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("a revoked device must be distinguishable, got %v", err)
	}
}

func TestOnlyTransientFailuresAreRetryable(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{400, false},
		{404, false},
		{409, false},
		{429, true},
		{500, true},
		{503, true},
	}

	for _, tc := range cases {
		e := &Error{Status: tc.status, Code: "x"}
		if e.Retryable() != tc.want {
			t.Errorf("status %d: retryable %v, want %v", tc.status, e.Retryable(), tc.want)
		}
	}
}

func TestPairWaitPollsUntilApproved(t *testing.T) {
	var calls int

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status": "approved",
			"token":  "nwa_secret",
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := c.PairWait(ctx, "poll-secret")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != PairApproved {
		t.Fatalf("want approved, got %q", res.Status)
	}
	if res.Token != "nwa_secret" {
		t.Fatalf("token not returned: %q", res.Token)
	}
	if calls < 3 {
		t.Fatalf("expected polling, got %d calls", calls)
	}
}

func TestPairWaitStopsOnExpiry(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "expired"})
	})

	res, err := c.PairWait(context.Background(), "poll-secret")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != PairExpired {
		t.Fatalf("want expired, got %q", res.Status)
	}
}

func TestPairWaitGivesUpOnPermanentError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": "bad_request"})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := c.PairWait(ctx, "poll-secret"); err == nil {
		t.Fatal("a permanent error must end the wait, not loop until timeout")
	}
}

func TestNoAuthHeaderBeforePairing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			t.Error("unpaired client must not send Authorization")
		}
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("version should be sent for support")
		}
		json.NewEncoder(w).Encode(map[string]any{"user_code": "ABCD-EFGH"})
	})

	if _, err := c.PairStart(context.Background(), "laptop", "windows"); err != nil {
		t.Fatal(err)
	}
}

func TestJitterStaysInBounds(t *testing.T) {
	for i := 0; i < 2000; i++ {
		d := jitter(4 * time.Second)
		if d < 2*time.Second || d >= 6*time.Second {
			t.Fatalf("jitter out of bounds: %v", d)
		}
	}
	if jitter(0) != 0 {
		t.Fatal("jitter(0) must be 0, not a panic")
	}
}
