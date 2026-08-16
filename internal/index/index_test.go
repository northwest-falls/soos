package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempIndex(t *testing.T) (*Index, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state", "index.gob")
	ix, _, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	return ix, p
}

var epoch = time.Unix(1_700_000_000, 0)

func TestMissingFileIsNotAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.gob")

	ix, recovered, err := Open(p)
	if err != nil {
		t.Fatalf("a missing index is normal, got %v", err)
	}
	if recovered {
		t.Fatal("a missing index is not a recovery")
	}
	if ix.Len() != 0 {
		t.Fatalf("want empty, got %d", ix.Len())
	}
}

func TestLookupHitRequiresAllThree(t *testing.T) {
	ix, _ := tempIndex(t)
	ix.Put("a.wav", Entry{Size: 100, Mod: epoch, Hash: "abc"})

	if _, ok := ix.Lookup("a.wav", 100, epoch); !ok {
		t.Fatal("exact match should hit")
	}
	if _, ok := ix.Lookup("a.wav", 101, epoch); ok {
		t.Fatal("a size change must miss")
	}
	if _, ok := ix.Lookup("a.wav", 100, epoch.Add(time.Second)); ok {
		t.Fatal("an mtime change must miss")
	}
	if _, ok := ix.Lookup("b.wav", 100, epoch); ok {
		t.Fatal("a different path must miss")
	}
}

func TestSaveAndReopen(t *testing.T) {
	ix, p := tempIndex(t)
	ix.Put("a.wav", Entry{Size: 100, Mod: epoch, Hash: "abc", Uploaded: true})
	ix.Put("b.wav", Entry{Size: 200, Mod: epoch, Hash: "def"})

	if err := ix.Save(); err != nil {
		t.Fatal(err)
	}
	if ix.Dirty() {
		t.Fatal("saving should clear dirty")
	}

	again, recovered, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if recovered {
		t.Fatal("a file we just wrote should not need recovery")
	}
	if again.Len() != 2 {
		t.Fatalf("want 2 entries, got %d", again.Len())
	}

	e, ok := again.Lookup("a.wav", 100, epoch)
	if !ok {
		t.Fatal("entry lost across save")
	}
	if e.Hash != "abc" || !e.Uploaded {
		t.Fatalf("entry came back wrong: %+v", e)
	}
}

func TestCorruptIndexRebuildsInsteadOfFailing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.gob")

	if err := os.WriteFile(p, []byte("this is not gob, it is nonsense"), 0o600); err != nil {
		t.Fatal(err)
	}

	ix, recovered, err := Open(p)
	if err != nil {
		t.Fatalf("corrupt index must not be an error, got %v", err)
	}
	if !recovered {
		t.Fatal("caller should be told it was discarded, so it can log it")
	}
	if ix.Len() != 0 {
		t.Fatalf("want empty after discard, got %d", ix.Len())
	}

	ix.Put("a.wav", Entry{Size: 1, Mod: epoch, Hash: "x"})
	if err := ix.Save(); err != nil {
		t.Fatalf("should recover into a working index: %v", err)
	}
}

func TestUploadedIsSeparateFromHashed(t *testing.T) {
	ix, _ := tempIndex(t)
	ix.Put("a.wav", Entry{Size: 1, Mod: epoch, Hash: "x"})

	e, _ := ix.Lookup("a.wav", 1, epoch)
	if e.Uploaded {
		t.Fatal("hashing must not imply uploaded, or a failed upload is never retried")
	}

	ix.MarkUploaded("a.wav")

	e, _ = ix.Lookup("a.wav", 1, epoch)
	if !e.Uploaded {
		t.Fatal("MarkUploaded did not stick")
	}
}

func TestKnownAtFindsRenames(t *testing.T) {
	ix, _ := tempIndex(t)
	ix.Put("old/name.wav", Entry{Size: 10, Mod: epoch, Hash: "samehash"})
	ix.Put("other.wav", Entry{Size: 99, Mod: epoch, Hash: "different"})

	got := ix.KnownAt("samehash")
	if len(got) != 1 || got[0] != "old/name.wav" {
		t.Fatalf("want the old path back, got %v", got)
	}

	if n := len(ix.KnownAt("nothing")); n != 0 {
		t.Fatalf("want none, got %d", n)
	}
}

func TestPruneDropsVanishedFiles(t *testing.T) {
	ix, _ := tempIndex(t)
	ix.Put("gone.wav", Entry{Size: 1, Mod: epoch, Hash: "a"})
	ix.Put("here.wav", Entry{Size: 1, Mod: epoch, Hash: "b"})

	dropped := ix.Prune(func(p string) bool { return p == "here.wav" })

	if dropped != 1 {
		t.Fatalf("want 1 dropped, got %d", dropped)
	}
	if ix.Len() != 1 {
		t.Fatalf("want 1 left, got %d", ix.Len())
	}
	if _, ok := ix.Lookup("here.wav", 1, epoch); !ok {
		t.Fatal("pruned the wrong one")
	}
}

func TestSaveIsAtomic(t *testing.T) {
	ix, p := tempIndex(t)
	ix.Put("a.wav", Entry{Size: 1, Mod: epoch, Hash: "first"})
	if err := ix.Save(); err != nil {
		t.Fatal(err)
	}

	ix.Put("a.wav", Entry{Size: 2, Mod: epoch, Hash: "second"})
	if err := ix.Save(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(p) {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}

	again, _, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := again.Lookup("a.wav", 2, epoch)
	if !ok || e.Hash != "second" {
		t.Fatalf("second save did not land: %+v ok=%v", e, ok)
	}
}

func TestFolderBindingIsIdentityNotTitle(t *testing.T) {
	ix, _ := tempIndex(t)

	ix.BindTrack("Bounces/Intro", "track_a", epoch)
	ix.BindTrack("Sessions/Intro", "track_b", epoch)

	a, ok := ix.TrackFor("Bounces/Intro")
	if !ok || a.TrackID != "track_a" {
		t.Fatalf("first Intro lost its binding: %+v ok=%v", a, ok)
	}

	b, ok := ix.TrackFor("Sessions/Intro")
	if !ok || b.TrackID != "track_b" {
		t.Fatal("two folders with the same name must stay two tracks")
	}

	if _, ok := ix.TrackFor("Bounces/Nights"); ok {
		t.Fatal("an unseen folder must not resolve to anything")
	}
}

func TestRebindSurvivesAFolderMove(t *testing.T) {
	ix, _ := tempIndex(t)
	ix.BindTrack("old/Intro", "track_a", epoch)

	if !ix.Rebind("old/Intro", "new/Intro") {
		t.Fatal("rebind should report success")
	}

	if _, ok := ix.TrackFor("old/Intro"); ok {
		t.Fatal("old path should be gone")
	}

	b, ok := ix.TrackFor("new/Intro")
	if !ok || b.TrackID != "track_a" {
		t.Fatal("a moved folder must keep pointing at the same track")
	}

	if ix.Rebind("nothing/here", "x") {
		t.Fatal("rebinding an unknown folder should report failure")
	}
}

func TestFoldersForTrackFindsTheOldPath(t *testing.T) {
	ix, _ := tempIndex(t)
	ix.BindTrack("a/Intro", "track_a", epoch)
	ix.BindTrack("b/Other", "track_b", epoch)

	got := ix.FoldersForTrack("track_a")
	if len(got) != 1 || got[0] != "a/Intro" {
		t.Fatalf("want the bound folder back, got %v", got)
	}
}

func TestBindingsSurviveSave(t *testing.T) {
	ix, p := tempIndex(t)
	ix.BindTrack("Bounces/Intro", "track_a", epoch)
	if err := ix.Save(); err != nil {
		t.Fatal(err)
	}

	again, recovered, err := Open(p)
	if err != nil || recovered {
		t.Fatalf("reopen failed: err=%v recovered=%v", err, recovered)
	}

	b, ok := again.TrackFor("Bounces/Intro")
	if !ok || b.TrackID != "track_a" {
		t.Fatal("bindings must survive a restart, or every scan re-asks")
	}
	if again.Bindings() != 1 {
		t.Fatalf("want 1 binding, got %d", again.Bindings())
	}
}

func TestUnbindForgets(t *testing.T) {
	ix, _ := tempIndex(t)
	ix.BindTrack("a", "t", epoch)
	ix.Unbind("a")

	if _, ok := ix.TrackFor("a"); ok {
		t.Fatal("unbind did not take")
	}
}
