package sync

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/northwest-falls/soos/internal/api"
	"github.com/northwest-falls/soos/internal/index"
	"github.com/northwest-falls/soos/internal/scan"
	"github.com/northwest-falls/soos/internal/watch"
)

type fakeAPI struct {
	holds map[string]bool

	haveCalls  int
	haveAsked  int
	uploaded   []string
	uploadReqs []api.InitRequest
	trackID    string
	failUpload error
	haveErr    error
}

func (f *fakeAPI) Have(_ context.Context, hashes []string) (map[string]bool, error) {
	if f.haveErr != nil {
		return nil, f.haveErr
	}
	f.haveCalls++
	f.haveAsked += len(hashes)

	out := map[string]bool{}
	for _, h := range hashes {
		if f.holds[h] {
			out[h] = true
		}
	}
	return out, nil
}

func (f *fakeAPI) UploadFile(_ context.Context, path string, req api.InitRequest,
	_ func(int64, int64)) (*api.InitResult, error) {
	if f.failUpload != nil {
		return nil, f.failUpload
	}
	f.uploaded = append(f.uploaded, path)
	f.uploadReqs = append(f.uploadReqs, req)

	id := f.trackID
	if id == "" {
		id = "track_default"
	}

	res := &api.InitResult{}
	res.Track.ID = id
	return res, nil
}

type diskEntry struct {
	path string
	info fs.FileInfo
}

func (e diskEntry) Path() string      { return e.path }
func (e diskEntry) Info() fs.FileInfo { return e.info }
func (e diskEntry) IsDir() bool       { return e.info.IsDir() }

func buildTree(t *testing.T, files map[string][]byte) (root string, list func(string) ([]scan.Entry, error)) {
	t.Helper()
	root = t.TempDir()

	for rel, data := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	list = func(string) ([]scan.Entry, error) {
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

	return root, list
}

func newSyncer(t *testing.T, root string, list func(string) ([]scan.Entry, error), a *fakeAPI) (*Syncer, *clock) {
	t.Helper()

	ix, _, err := index.Open(filepath.Join(t.TempDir(), "index.gob"))
	if err != nil {
		t.Fatal(err)
	}

	c := &clock{t: time.Unix(1_700_000_000, 0)}

	set := watch.NewSettler()
	set.Now = c.now

	return &Syncer{
		Root: root, Index: ix, Settler: set, API: a, List: list,
		Now: c.now,
	}, c
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func settle(t *testing.T, s *Syncer, c *clock) *Outcome {
	t.Helper()
	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.add(watch.DefaultQuiet * 2)
	out, err := s.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUploadsOnlyWhatTheVaultLacks(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{
		"Nights/take1.wav": []byte("first bounce"),
		"Nights/take2.wav": []byte("second bounce"),
	})

	a := &fakeAPI{holds: map[string]bool{}}
	s, c := newSyncer(t, root, list, a)

	out := settle(t, s, c)

	if out.Uploaded != 2 {
		t.Fatalf("want 2 uploaded, got %d (waiting %d failed %d, errs %v)", out.Uploaded, out.Waiting, out.Failed, out.Errors)
	}
	if a.haveCalls != 1 {
		t.Fatalf("one pass should ask once, not per file: %d calls", a.haveCalls)
	}
}

func TestNeverUploadsWhatTheServerAlreadyHolds(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{
		"Nights/take1.wav": []byte("already ours"),
	})

	a := &fakeAPI{holds: map[string]bool{}}
	s, c := newSyncer(t, root, list, a)

	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.add(watch.DefaultQuiet * 2)

	pre := &fakeAPI{holds: map[string]bool{}}
	s2, c2 := newSyncer(t, root, list, pre)
	settle(t, s2, c2)
	if len(pre.uploadReqs) != 1 {
		t.Fatalf("setup: expected one upload, got %d", len(pre.uploadReqs))
	}
	hash := pre.uploadReqs[0].ContentHash

	a.holds[hash] = true
	out, err := s.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if out.Uploaded != 0 {
		t.Fatalf("must not upload what the vault holds, uploaded %d", out.Uploaded)
	}
	if out.Deduped != 1 {
		t.Fatalf("want 1 deduped, got %d", out.Deduped)
	}
	if len(a.uploaded) != 0 {
		t.Fatalf("no bytes should have moved, got %v", a.uploaded)
	}
}

func TestSecondPassOpensNothing(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{
		"Nights/take1.wav": []byte("a bounce"),
		"Nights/take2.wav": []byte("another"),
	})

	a := &fakeAPI{holds: map[string]bool{}}
	s, c := newSyncer(t, root, list, a)

	settle(t, s, c)

	a.haveCalls = 0
	c.add(time.Hour)

	out, err := s.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if out.Skipped != 2 {
		t.Fatalf("want both answered by the index, got %d skipped", out.Skipped)
	}
	if out.Uploaded != 0 || out.Deduped != 0 {
		t.Fatalf("nothing should move on an unchanged folder: %+v", out)
	}
	if a.haveCalls != 0 {
		t.Fatal("an unchanged folder must not even ask the server")
	}
}

func TestFolderBindsToTrackAndReusesIt(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{
		"Nights/take1.wav": []byte("first"),
	})

	a := &fakeAPI{holds: map[string]bool{}, trackID: "track_nights"}
	s, c := newSyncer(t, root, list, a)
	settle(t, s, c)

	folder := filepath.Join(root, "Nights")
	b, ok := s.Index.TrackFor(folder)
	if !ok || b.TrackID != "track_nights" {
		t.Fatalf("folder should be bound after the first upload: %+v ok=%v", b, ok)
	}

	if err := os.WriteFile(filepath.Join(folder, "take2.wav"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.add(time.Hour)
	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.add(watch.DefaultQuiet * 2)
	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}

	last := a.uploadReqs[len(a.uploadReqs)-1]
	if last.TrackID != "track_nights" {
		t.Fatalf("second version must carry the bound track id, got %q", last.TrackID)
	}
}

func TestFailedUploadIsNotMarkedDone(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{
		"Nights/take1.wav": []byte("a bounce"),
	})

	a := &fakeAPI{holds: map[string]bool{}, failUpload: errors.New("network gone")}
	s, c := newSyncer(t, root, list, a)

	out := settle(t, s, c)

	if out.Failed != 1 {
		t.Fatalf("want 1 failure, got %+v", out)
	}

	e, ok := s.Index.Lookup(filepath.Join(root, "Nights", "take1.wav"), 8, time.Time{})
	_ = e
	_ = ok

	for _, p := range []string{filepath.Join(root, "Nights", "take1.wav")} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := s.Index.Lookup(p, fi.Size(), fi.ModTime())
		if !ok {
			t.Fatal("a hashed file should be recorded so it is not rehashed")
		}
		if got.Uploaded {
			t.Fatal("a failed upload must not be marked uploaded, or it is never retried")
		}
	}
}

func TestRevokedDeviceStopsThePassImmediately(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{
		"A/one.wav": []byte("1"), "B/two.wav": []byte("22"), "C/three.wav": []byte("333"),
	})

	a := &fakeAPI{holds: map[string]bool{}, failUpload: api.ErrUnauthorized}
	s, c := newSyncer(t, root, list, a)

	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.add(watch.DefaultQuiet * 2)

	out, err := s.Once(context.Background())
	if !errors.Is(err, api.ErrUnauthorized) {
		t.Fatalf("want the revocation surfaced, got %v", err)
	}
	if out.Failed != 1 {
		t.Fatalf("should stop after the first refusal, not try all three: failed=%d", out.Failed)
	}
}

func TestDocumentsNeverReachTheUploader(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{
		"Nights/take1.wav": []byte("audio"),
		"Nights/deal.pdf":  []byte("%PDF-1.4 contract"),
		"Nights/notes.txt": []byte("lyrics"),
		"Nights/clip.mp4":  []byte("video"),
	})

	a := &fakeAPI{holds: map[string]bool{}}
	s, c := newSyncer(t, root, list, a)

	out := settle(t, s, c)

	if out.Uploaded != 1 {
		t.Fatalf("only the audio should upload, got %d", out.Uploaded)
	}
	for _, p := range a.uploaded {
		if filepath.Ext(p) != ".wav" {
			t.Fatalf("a non-audio file reached the uploader: %s", p)
		}
	}
	if out.Ignored["document, never uploaded"] != 2 {
		t.Fatalf("both documents should be reported, got %v", out.Ignored)
	}
}

func TestHaveFailureLeavesWorkForNextPass(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{
		"Nights/take1.wav": []byte("a bounce"),
	})

	a := &fakeAPI{holds: map[string]bool{}, haveErr: errors.New("offline")}
	s, c := newSyncer(t, root, list, a)

	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.add(watch.DefaultQuiet * 2)

	if _, err := s.Once(context.Background()); err == nil {
		t.Fatal("a failed Have should surface")
	}

	p := filepath.Join(root, "Nights", "take1.wav")
	fi, _ := os.Stat(p)
	e, ok := s.Index.Lookup(p, fi.Size(), fi.ModTime())
	if !ok || e.Hash == "" {
		t.Fatal("the hash should have been kept so the next pass does not redo it")
	}
	if e.Uploaded {
		t.Fatal("nothing was uploaded")
	}
}

func TestStorageFullPausesInsteadOfGrinding(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{
		"A/one.wav": []byte("1"), "B/two.wav": []byte("22"), "C/three.wav": []byte("333"),
	})

	a := &fakeAPI{holds: map[string]bool{}, failUpload: api.ErrStorageFull}
	s, c := newSyncer(t, root, list, a)

	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.add(watch.DefaultQuiet * 2)

	out, _ := s.Once(context.Background())

	if !out.Paused {
		t.Fatal("running out of space should pause the pass")
	}
	if out.Stopped {
		t.Fatal("out of space is not terminal; nothing was lost and nothing was revoked")
	}
	if out.Failed != 1 {
		t.Fatalf("should stop after the first refusal, got %d failures", out.Failed)
	}
	if d := out.NextCheck(time.Minute); d < 10*time.Minute {
		t.Fatalf("a paused agent must back right off, got %v", d)
	}
}

func TestReadOnlyAccountIsPausedNotUnpaired(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{"A/one.wav": []byte("1")})

	a := &fakeAPI{holds: map[string]bool{}, failUpload: api.ErrReadOnly}
	s, c := newSyncer(t, root, list, a)

	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.add(watch.DefaultQuiet * 2)

	out, _ := s.Once(context.Background())

	if out.Stopped {
		t.Fatal("a read-only account must never be reported as unpaired")
	}
	if !out.Paused {
		t.Fatal("a read-only account should pause")
	}
}

func TestRevokedDeviceIsTerminal(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{"A/one.wav": []byte("1")})

	a := &fakeAPI{holds: map[string]bool{}, failUpload: api.ErrUnauthorized}
	s, c := newSyncer(t, root, list, a)

	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.add(watch.DefaultQuiet * 2)

	out, _ := s.Once(context.Background())

	if !out.Stopped {
		t.Fatal("a removed device is terminal")
	}
	if d := out.NextCheck(time.Minute); d != 0 {
		t.Fatalf("nothing to poll for once unpaired, got %v", d)
	}
}

func TestNothingLocalIsEverRemoved(t *testing.T) {
	root, list := buildTree(t, map[string][]byte{"A/one.wav": []byte("keep me")})

	a := &fakeAPI{holds: map[string]bool{}, failUpload: api.ErrStorageFull}
	s, c := newSyncer(t, root, list, a)

	s.Once(context.Background())
	c.add(watch.DefaultQuiet * 2)
	s.Once(context.Background())

	b, err := os.ReadFile(filepath.Join(root, "A", "one.wav"))
	if err != nil {
		t.Fatalf("the file must still be there: %v", err)
	}
	if string(b) != "keep me" {
		t.Fatal("the file was modified")
	}
}

func TestIntervalBacksOffWhenIdleAndSnapsBackWhenBusy(t *testing.T) {
	idle := &Outcome{}

	d := idle.NextCheck(MinInterval)
	if d != MinInterval*2 {
		t.Fatalf("an idle pass should double, got %v", d)
	}

	for i := 0; i < 20; i++ {
		d = idle.NextCheck(d)
	}
	if d != MaxInterval {
		t.Fatalf("want the ceiling %v, got %v", MaxInterval, d)
	}

	busy := &Outcome{Uploaded: 1}
	if got := busy.NextCheck(d); got != MinInterval {
		t.Fatalf("a busy pass must snap back to %v, got %v", MinInterval, got)
	}

	waiting := &Outcome{Waiting: 1}
	if got := waiting.NextCheck(MaxInterval); got != MinInterval {
		t.Fatalf("a file mid-write means look again soon, got %v", got)
	}
}

func TestPausedAndStoppedIgnoreTheBackoff(t *testing.T) {
	if got := (&Outcome{Paused: true}).NextCheck(MinInterval); got < 10*time.Minute {
		t.Fatalf("paused must back right off, got %v", got)
	}
	if got := (&Outcome{Stopped: true}).NextCheck(MinInterval); got != 0 {
		t.Fatalf("stopped has nothing to poll for, got %v", got)
	}
}
