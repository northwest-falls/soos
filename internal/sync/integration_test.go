package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/northwest-falls/soos/internal/api"
	"github.com/northwest-falls/soos/internal/contenthash"
	"github.com/northwest-falls/soos/internal/index"
	"github.com/northwest-falls/soos/internal/scan"
	"github.com/northwest-falls/soos/internal/watch"
)

type worker struct {
	mu sync.Mutex

	held      map[string]bool
	partSize  int64
	assembled map[string][]byte
	completed map[string]bool
	tokens    map[string]string
	seq       int
}

func newWorker(partSize int64) *worker {
	return &worker{
		held:      map[string]bool{},
		partSize:  partSize,
		assembled: map[string][]byte{},
		completed: map[string]bool{},
		tokens:    map[string]string{},
	}
}

func (w *worker) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/agent/have", func(rw http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer nwa_test" {
			t.Errorf("have: missing or wrong bearer: %q", got)
		}

		var in struct {
			Hashes []string `json:"hashes"`
		}
		json.NewDecoder(r.Body).Decode(&in)

		w.mu.Lock()
		var have []string
		for _, h := range in.Hashes {
			if w.held[h] {
				have = append(have, h)
			}
		}
		w.mu.Unlock()

		json.NewEncoder(rw).Encode(map[string]any{"have": have})
	})

	mux.HandleFunc("/api/upload/init", func(rw http.ResponseWriter, r *http.Request) {
		var in api.InitRequest
		json.NewDecoder(r.Body).Decode(&in)

		if in.ContentHash == "" {
			t.Error("init: no content hash sent, so the server cannot dedupe")
		}
		if in.Kind != "master" {
			t.Errorf("init: want kind master, got %q", in.Kind)
		}

		w.mu.Lock()
		w.seq++
		token := fmt.Sprintf("tok-%d", w.seq)
		w.tokens[token] = in.Filename
		multipart := in.ByteSize > w.partSize
		count := 1
		if multipart {
			count = int((in.ByteSize + w.partSize - 1) / w.partSize)
		}
		w.mu.Unlock()

		json.NewEncoder(rw).Encode(map[string]any{
			"upload": map[string]any{
				"asset_id": token, "token": token,
				"multipart": multipart, "part_size": w.partSize, "part_count": count,
			},
			"track":   map[string]any{"id": "trk_" + token, "title": "T"},
			"version": map[string]any{"id": "ver_" + token, "number": 1},
		})
	})

	mux.HandleFunc("/api/upload/part", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("part: want PUT, got %s", r.Method)
		}
		if r.ContentLength <= 0 {
			rw.WriteHeader(http.StatusLengthRequired)
			return
		}

		token := r.URL.Query().Get("token")
		part, _ := strconv.Atoi(r.URL.Query().Get("part"))

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(body)) != r.ContentLength {
			t.Errorf("part %d: declared %d, got %d", part, r.ContentLength, len(body))
		}

		w.mu.Lock()

		w.assembled[token] = append(w.assembled[token], body...)
		w.mu.Unlock()

		json.NewEncoder(rw).Encode(map[string]any{"part": part, "etag": "e"})
	})

	mux.HandleFunc("/api/upload/complete", func(rw http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		json.NewDecoder(r.Body).Decode(&in)

		w.mu.Lock()
		w.completed[in.Token] = true
		body := w.assembled[in.Token]
		w.mu.Unlock()

		h, err := contenthash.Reader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}

		w.mu.Lock()
		w.held[h] = true
		w.mu.Unlock()

		json.NewEncoder(rw).Encode(map[string]any{"ok": true})
	})

	return mux
}

func TestEndToEndBytesArriveIntact(t *testing.T) {

	w := newWorker(4096)
	srv := httptest.NewServer(w.handler(t))
	defer srv.Close()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Nights"), 0o755); err != nil {
		t.Fatal(err)
	}

	big := make([]byte, 10_000)
	for i := range big {
		big[i] = byte(i % 251)
	}
	small := []byte("a short bounce")

	files := map[string][]byte{
		filepath.Join(root, "Nights", "take1.wav"): big,
		filepath.Join(root, "Nights", "take2.wav"): small,

		filepath.Join(root, "Nights", "contract.pdf"): []byte("%PDF-1.4"),
	}
	for p, b := range files {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := api.New(srv.URL, "test")
	c.Token = "nwa_test"
	c.HTTP = srv.Client()

	ix, _, err := index.Open(filepath.Join(t.TempDir(), "index.gob"))
	if err != nil {
		t.Fatal(err)
	}

	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	set := watch.NewSettler()
	set.Now = clk.now

	s := &Syncer{
		Root: root, Index: ix, Settler: set, API: c,
		List: listReal(root), Now: clk.now,
	}

	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk.add(watch.DefaultQuiet * 2)

	out, err := s.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if out.Uploaded != 2 {
		t.Fatalf("want 2 uploaded, got %+v", out)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.assembled) != 2 {
		t.Fatalf("want 2 uploads at the server, got %d", len(w.assembled))
	}

	var sawBig, sawSmall bool
	for token, got := range w.assembled {
		if !w.completed[token] {
			t.Errorf("%s was never completed", token)
		}
		switch len(got) {
		case len(big):
			if !bytes.Equal(got, big) {
				t.Error("the multipart file did not reassemble byte for byte")
			}
			sawBig = true
		case len(small):
			if !bytes.Equal(got, small) {
				t.Error("the single-part file did not arrive intact")
			}
			sawSmall = true
		default:
			t.Errorf("unexpected upload of %d bytes", len(got))
		}
	}
	if !sawBig || !sawSmall {
		t.Fatal("expected one multipart and one single-part upload")
	}
}

func TestEndToEndSecondMachineUploadsNothing(t *testing.T) {
	w := newWorker(4096)
	srv := httptest.NewServer(w.handler(t))
	defer srv.Close()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Nights"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Nights", "take1.wav"), []byte("the only bounce"), 0o644); err != nil {
		t.Fatal(err)
	}

	newSyncerFor := func() (*Syncer, *clock) {
		c := api.New(srv.URL, "test")
		c.Token = "nwa_test"
		c.HTTP = srv.Client()

		ix, _, err := index.Open(filepath.Join(t.TempDir(), "index.gob"))
		if err != nil {
			t.Fatal(err)
		}

		clk := &clock{t: time.Unix(1_700_000_000, 0)}
		set := watch.NewSettler()
		set.Now = clk.now

		return &Syncer{Root: root, Index: ix, Settler: set, API: c,
			List: listReal(root), Now: clk.now}, clk
	}

	first, clk := newSyncerFor()
	first.Once(context.Background())
	clk.add(watch.DefaultQuiet * 2)
	out, err := first.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Uploaded != 1 {
		t.Fatalf("first machine should upload once, got %+v", out)
	}

	second, clk2 := newSyncerFor()
	second.Once(context.Background())
	clk2.add(watch.DefaultQuiet * 2)
	out2, err := second.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if out2.Uploaded != 0 {
		t.Fatalf("a second machine must upload nothing, got %d", out2.Uploaded)
	}
	if out2.Deduped != 1 {
		t.Fatalf("want it recognised as already held, got %+v", out2)
	}
}

func listReal(root string) func(string) ([]scan.Entry, error) {
	return func(string) ([]scan.Entry, error) {
		var out []scan.Entry
		err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			out = append(out, diskEntry{p, fi})
			return nil
		})
		return out, err
	}
}
