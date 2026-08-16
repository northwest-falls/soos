package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type fakeServer struct {
	mu        sync.Mutex
	parts     map[int][]byte
	completed bool
	partSize  int64
	multipart bool
	failPart  int
	failTimes int
}

func (s *fakeServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/upload/init":
			var in InitRequest
			json.NewDecoder(r.Body).Decode(&in)

			count := 1
			if s.multipart {
				count = int((in.ByteSize + s.partSize - 1) / s.partSize)
			}

			json.NewEncoder(w).Encode(map[string]any{
				"upload": map[string]any{
					"asset_id":   "asset_1",
					"token":      "tok/with+chars=",
					"multipart":  s.multipart,
					"part_size":  s.partSize,
					"part_count": count,
				},
				"track":   map[string]any{"id": "t1", "title": "Nights"},
				"version": map[string]any{"id": "v1", "number": 1},
			})

		case r.URL.Path == "/api/upload/part":
			q := r.URL.Query()
			if q.Get("token") != "tok/with+chars=" {
				t.Errorf("token not round-tripped, got %q", q.Get("token"))
			}
			part, _ := fmt.Sscanf(q.Get("part"), "%d", new(int))
			_ = part

			n := 0
			fmt.Sscanf(q.Get("part"), "%d", &n)

			s.mu.Lock()
			if n == s.failPart && s.failTimes > 0 {
				s.failTimes--
				s.mu.Unlock()
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			s.mu.Unlock()

			if r.ContentLength <= 0 {
				w.WriteHeader(http.StatusLengthRequired)
				return
			}

			body, _ := io.ReadAll(r.Body)
			if int64(len(body)) != r.ContentLength {
				t.Errorf("part %d: declared %d bytes, sent %d", n, r.ContentLength, len(body))
			}

			s.mu.Lock()
			s.parts[n] = body
			s.mu.Unlock()

			json.NewEncoder(w).Encode(map[string]any{"part": n, "etag": "e"})

		case r.URL.Path == "/api/upload/complete":
			s.mu.Lock()
			s.completed = true
			s.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"ok": true})

		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "master.wav")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (s *fakeServer) assembled() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []byte
	for i := 1; i <= len(s.parts); i++ {
		out = append(out, s.parts[i]...)
	}
	return out
}

func TestMultipartUploadReassemblesExactly(t *testing.T) {
	data := make([]byte, 10_000)
	for i := range data {
		data[i] = byte(i % 251)
	}

	s := &fakeServer{parts: map[int][]byte{}, partSize: 3000, multipart: true}
	c := newTestClient(t, s.handler(t))

	var lastSent, lastTotal int64
	_, err := c.UploadFile(context.Background(), writeTemp(t, data),
		InitRequest{Filename: "master.wav", ContentHash: "h"},
		func(sent, total int64) { lastSent, lastTotal = sent, total })
	if err != nil {
		t.Fatal(err)
	}

	if got := s.assembled(); !bytes.Equal(got, data) {
		t.Fatalf("reassembled file differs: got %d bytes, want %d", len(got), len(data))
	}
	if len(s.parts) != 4 {
		t.Fatalf("10000 bytes at 3000 per part should be 4 parts, got %d", len(s.parts))
	}
	if !s.completed {
		t.Fatal("complete was never called")
	}
	if lastSent != int64(len(data)) || lastTotal != int64(len(data)) {
		t.Fatalf("progress ended at %d/%d", lastSent, lastTotal)
	}
}

func TestSinglePartUpload(t *testing.T) {
	data := []byte("a short bounce")

	s := &fakeServer{parts: map[int][]byte{}, multipart: false}
	c := newTestClient(t, s.handler(t))

	if _, err := c.UploadFile(context.Background(), writeTemp(t, data),
		InitRequest{Filename: "master.wav", ContentHash: "h"}, nil); err != nil {
		t.Fatal(err)
	}

	if got := s.assembled(); !bytes.Equal(got, data) {
		t.Fatalf("single-part upload differs: %q vs %q", got, data)
	}
}

func TestRetriedPartResendsIdenticalBytes(t *testing.T) {
	data := make([]byte, 9000)
	for i := range data {
		data[i] = byte(i % 97)
	}

	s := &fakeServer{
		parts: map[int][]byte{}, partSize: 3000, multipart: true,
		failPart: 2, failTimes: 2,
	}
	c := newTestClient(t, s.handler(t))

	if _, err := c.UploadFile(context.Background(), writeTemp(t, data),
		InitRequest{Filename: "master.wav", ContentHash: "h"}, nil); err != nil {
		t.Fatal(err)
	}

	if got := s.assembled(); !bytes.Equal(got, data) {
		t.Fatalf("file corrupted by retry: got %d bytes, want %d", len(got), len(data))
	}
}

func TestSizeIsTakenFromDiskNotTheCaller(t *testing.T) {
	data := make([]byte, 5000)

	var declared int64
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/upload/init" {
			var in InitRequest
			json.NewDecoder(r.Body).Decode(&in)
			declared = in.ByteSize
			json.NewEncoder(w).Encode(map[string]any{
				"upload":  map[string]any{"token": "t", "multipart": false, "part_count": 1},
				"track":   map[string]any{},
				"version": map[string]any{},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	_, err := c.UploadFile(context.Background(), writeTemp(t, data),
		InitRequest{Filename: "master.wav", ByteSize: 999_999, ContentHash: "h"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if declared != int64(len(data)) {
		t.Fatalf("declared %d, want the real size %d", declared, len(data))
	}
}

func TestKindDefaultsToMaster(t *testing.T) {
	var kind string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var in InitRequest
		json.NewDecoder(r.Body).Decode(&in)
		kind = in.Kind
		json.NewEncoder(w).Encode(map[string]any{
			"upload": map[string]any{"token": "t", "part_count": 1}, "track": map[string]any{}, "version": map[string]any{},
		})
	})

	c.UploadInit(context.Background(), InitRequest{Filename: "a.wav"})

	if kind != "master" {
		t.Fatalf("want master, got %q", kind)
	}
}

func TestSimilarTrackIsSurfacedNotActedOn(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"upload":        map[string]any{"token": "t", "part_count": 1},
			"track":         map[string]any{"id": "new", "title": "Intro"},
			"version":       map[string]any{"id": "v", "number": 1},
			"similar_track": map[string]any{"id": "old", "title": "Intro"},
		})
	})

	res, err := c.UploadInit(context.Background(), InitRequest{Filename: "a.wav"})
	if err != nil {
		t.Fatal(err)
	}

	if res.SimilarTrack == nil || res.SimilarTrack.ID != "old" {
		t.Fatal("similar_track must reach the caller so it can ask")
	}
	if res.Track.ID != "new" {
		t.Fatal("nothing should have been merged server side")
	}
}

func TestTokenIsEscapedInTheQuery(t *testing.T) {
	raw := "abc/def+ghi=jkl&mno"
	escaped := urlQueryEscape(raw)

	parsed, err := url.ParseQuery("token=" + escaped)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Get("token") != raw {
		t.Fatalf("token did not survive the round trip: %q", parsed.Get("token"))
	}
}
