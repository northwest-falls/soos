package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestSettler() (*Settler, *clock) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := NewSettler()
	s.Now = c.now
	return s, c
}

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGrowingFileIsNeverReady(t *testing.T) {
	s, c := newTestSettler()
	path := filepath.Join(t.TempDir(), "bounce.wav")

	write(t, path, 1024)

	if got, _, err := s.Check(path); err != nil || got != NotReady {
		t.Fatalf("first sighting should never be ready: got %v err %v", got, err)
	}

	for i := 0; i < 5; i++ {
		c.add(10 * time.Second)
		write(t, path, 2048*(i+2))

		got, _, err := s.Check(path)
		if err != nil {
			t.Fatal(err)
		}
		if got == Ready {
			t.Fatalf("a file that changed since the last look must not be Ready (iteration %d)", i)
		}
	}
}

func TestReadyOnlyAfterQuietPeriod(t *testing.T) {
	s, c := newTestSettler()
	path := filepath.Join(t.TempDir(), "master.wav")

	write(t, path, 4096)

	if got, _, _ := s.Check(path); got != NotReady {
		t.Fatalf("want NotReady on first sighting, got %v", got)
	}

	c.add(DefaultQuiet - time.Millisecond)
	if got, _, _ := s.Check(path); got != NotReady {
		t.Fatalf("want NotReady before the quiet period elapses, got %v", got)
	}

	c.add(2 * time.Millisecond)
	got, info, err := s.Check(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != Ready {
		t.Fatalf("want Ready once size and mtime held still, got %v", got)
	}
	if info == nil || info.Size() != 4096 {
		t.Fatalf("want the settled info back, got %v", info)
	}
}

func TestSettledFileIsForgotten(t *testing.T) {
	s, c := newTestSettler()
	path := filepath.Join(t.TempDir(), "a.wav")

	write(t, path, 4096)
	s.Check(path)
	c.add(DefaultQuiet * 2)

	if got, _, _ := s.Check(path); got != Ready {
		t.Fatal("expected Ready")
	}

	if n := s.Tracking(); n != 0 {
		t.Fatalf("a settled file should be dropped from memory, still tracking %d", n)
	}
}

func TestVanishedFile(t *testing.T) {
	s, _ := newTestSettler()
	path := filepath.Join(t.TempDir(), "gone.wav")

	got, _, err := s.Check(path)
	if err != nil {
		t.Fatalf("a missing file is an answer, not an error: %v", err)
	}
	if got != Vanished {
		t.Fatalf("want Vanished, got %v", got)
	}
}

func TestDirectoriesAreNotFiles(t *testing.T) {
	s, _ := newTestSettler()
	dir := t.TempDir()

	if got, _, _ := s.Check(dir); got == Ready {
		t.Fatal("a directory must never settle as Ready")
	}
}

func TestTrackingCountsOnlyFilesInFlight(t *testing.T) {
	s, _ := newTestSettler()
	dir := t.TempDir()

	for i := 0; i < 20; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".wav")
		write(t, p, 1024)
		s.Check(p)
	}

	if n := s.Tracking(); n != 20 {
		t.Fatalf("want 20 in flight, got %d", n)
	}

	for i := 0; i < 20; i++ {
		s.Forget(filepath.Join(dir, string(rune('a'+i))+".wav"))
	}

	if n := s.Tracking(); n != 0 {
		t.Fatalf("want 0 after forgetting, got %d", n)
	}
}
