package scan

import (
	"io/fs"
	"path/filepath"
	"testing"
	"time"
)

type fakeInfo struct {
	name string
	size int64
	mod  time.Time
	dir  bool
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() fs.FileMode  { return 0 }
func (f fakeInfo) ModTime() time.Time { return f.mod }
func (f fakeInfo) IsDir() bool        { return f.dir }
func (f fakeInfo) Sys() any           { return nil }

type fakeEntry struct {
	path string
	info fakeInfo
}

func (e fakeEntry) Path() string      { return e.path }
func (e fakeEntry) Info() fs.FileInfo { return e.info }
func (e fakeEntry) IsDir() bool       { return e.info.dir }

var root = filepath.FromSlash("/music/Bounces")
var t0 = time.Unix(1_700_000_000, 0)

func classify(t *testing.T, rel string, opts Options) Candidate {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	return Classify(root, p, fakeInfo{name: filepath.Base(p), size: 1000, mod: t0}, opts)
}

func TestAudioInATrackFolderIsAVersion(t *testing.T) {
	c := classify(t, "Nights Without You/take3.wav", Options{})

	if c.Kind != Version {
		t.Fatalf("want version, got %v (%s)", c.Kind, c.Reason)
	}
	if c.Title != "Nights Without You" {
		t.Fatalf("title must come from the folder, got %q", c.Title)
	}
}

func TestCreativeFilenamesAreStillVersions(t *testing.T) {
	for _, name := range []string{
		"ahvbdghrkv2.wav", "4hb4bwkwhb.wav",
		"final_final_REAL_v3.wav", "asdfasdf.aiff",
	} {
		c := classify(t, "Nights Without You/"+name, Options{})
		if c.Kind != Version {
			t.Errorf("%s: want version, got %v (%s)", name, c.Kind, c.Reason)
		}
		if c.Title != "Nights Without You" {
			t.Errorf("%s: title should be the folder, got %q", name, c.Title)
		}
	}
}

func TestNestedAudioIsAStemNotAVersion(t *testing.T) {
	c := classify(t, "Nights Without You/stems/kick.wav", Options{})

	if c.Kind != Stem {
		t.Fatalf("nested audio must be a stem, got %v", c.Kind)
	}
	if c.Title != "Nights Without You" {
		t.Fatalf("stem should belong to the track folder, got %q", c.Title)
	}
}

func TestLooseAudioInTheRootIsItsOwnTrack(t *testing.T) {
	c := classify(t, "quick idea.wav", Options{})

	if c.Kind != Version {
		t.Fatalf("want version, got %v", c.Kind)
	}
	if c.Title != "quick idea" {
		t.Fatalf("want the filename as title with no extension, got %q", c.Title)
	}
}

func TestDocumentsAreNeverOffered(t *testing.T) {
	for _, name := range []string{"contract.pdf", "split sheet.docx", "invoice.xlsx", "notes.txt"} {
		c := classify(t, "Nights Without You/"+name, Options{Projects: true, Artwork: true})
		if c.Kind != Ignored {
			t.Errorf("%s: must never be uploaded, got %v", name, c.Kind)
		}
		if c.Reason != "document, never uploaded" {
			t.Errorf("%s: reason should be specific, got %q", name, c.Reason)
		}
	}
}

func TestVideoIsNeverOffered(t *testing.T) {
	c := classify(t, "Nights Without You/livestream.mp4", Options{Projects: true, Artwork: true})
	if c.Kind != Ignored {
		t.Fatalf("video must never be uploaded, got %v", c.Kind)
	}
}

func TestProjectsAndArtworkAreOffByDefault(t *testing.T) {
	if c := classify(t, "Nights Without You/session.flp", Options{}); c.Kind != Ignored {
		t.Fatalf("projects must be off by default, got %v", c.Kind)
	}
	if c := classify(t, "Nights Without You/cover.jpg", Options{}); c.Kind != Ignored {
		t.Fatalf("artwork must be off by default, got %v", c.Kind)
	}

	if c := classify(t, "Nights Without You/session.flp", Options{Projects: true}); c.Kind != Project {
		t.Fatalf("projects should switch on, got %v", c.Kind)
	}
	if c := classify(t, "Nights Without You/cover.jpg", Options{Artwork: true}); c.Kind != Artwork {
		t.Fatalf("artwork should switch on, got %v", c.Kind)
	}
}

func TestDawLitterIsSilentlySkipped(t *testing.T) {
	for _, name := range []string{"take3.wav.asd", ".DS_Store", "Thumbs.db", "bounce.wav.tmp", ".hidden.wav"} {
		c := classify(t, "Nights Without You/"+name, Options{})
		if c.Kind != Ignored {
			t.Errorf("%s: should be skipped, got %v", name, c.Kind)
		}
		if c.Reason != "noise" {
			t.Errorf("%s: litter should be silent, got reason %q", name, c.Reason)
		}
	}
}

func TestWalkOrdersByTimeNotName(t *testing.T) {
	dir := filepath.Join(root, "Nights Without You")

	entries := []Entry{
		fakeEntry{filepath.Join(dir, "zzz_first.wav"), fakeInfo{name: "zzz_first.wav", size: 1, mod: t0}},
		fakeEntry{filepath.Join(dir, "aaa_last.wav"), fakeInfo{name: "aaa_last.wav", size: 1, mod: t0.Add(2 * time.Hour)}},
		fakeEntry{filepath.Join(dir, "mmm_middle.wav"), fakeInfo{name: "mmm_middle.wav", size: 1, mod: t0.Add(time.Hour)}},
	}

	res, err := Walk(root, Options{}, func(string) ([]Entry, error) { return entries, nil })
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Candidates) != 3 {
		t.Fatalf("want 3, got %d", len(res.Candidates))
	}

	want := []string{"zzz_first.wav", "mmm_middle.wav", "aaa_last.wav"}
	for i, w := range want {
		if got := filepath.Base(res.Candidates[i].Path); got != w {
			t.Fatalf("position %d: want %s, got %s (sorted by name, not time)", i, w, got)
		}
	}
}

func TestWalkReportsWhatItSkipped(t *testing.T) {
	dir := filepath.Join(root, "Nights Without You")

	entries := []Entry{
		fakeEntry{filepath.Join(dir, "take.wav"), fakeInfo{name: "take.wav", size: 1, mod: t0}},
		fakeEntry{filepath.Join(dir, "contract.pdf"), fakeInfo{name: "contract.pdf", size: 1, mod: t0}},
		fakeEntry{filepath.Join(dir, "deal.pdf"), fakeInfo{name: "deal.pdf", size: 1, mod: t0}},
		fakeEntry{filepath.Join(dir, "clip.mp4"), fakeInfo{name: "clip.mp4", size: 1, mod: t0}},
		fakeEntry{filepath.Join(dir, ".DS_Store"), fakeInfo{name: ".DS_Store", size: 1, mod: t0}},
	}

	res, err := Walk(root, Options{}, func(string) ([]Entry, error) { return entries, nil })
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Candidates) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(res.Candidates))
	}
	if res.Skipped["document, never uploaded"] != 2 {
		t.Fatalf("want 2 documents reported, got %v", res.Skipped)
	}
	if res.Skipped["video, never uploaded"] != 1 {
		t.Fatalf("want the video reported, got %v", res.Skipped)
	}

	for reason := range res.Skipped {
		if reason == "noise" {
			t.Fatal("litter should be silent")
		}
	}
}

func TestWalkSkipsDirectories(t *testing.T) {
	entries := []Entry{
		fakeEntry{filepath.Join(root, "Nights"), fakeInfo{name: "Nights", dir: true}},
	}

	res, err := Walk(root, Options{}, func(string) ([]Entry, error) { return entries, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 0 {
		t.Fatalf("directories are not candidates, got %d", len(res.Candidates))
	}
}
