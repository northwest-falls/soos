package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/northwest-falls/soos/internal/scan"
	"github.com/northwest-falls/soos/internal/watch"
)

func buildCatalogue(tb testing.TB, folders, perFolder, size int) (string, func(string) ([]scan.Entry, error)) {
	tb.Helper()
	root := tb.TempDir()

	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i % 251)
	}

	for f := 0; f < folders; f++ {
		dir := filepath.Join(root, fmt.Sprintf("Track %02d", f))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatal(err)
		}
		for v := 0; v < perFolder; v++ {

			buf[0] = byte(f*257 + v)
			p := filepath.Join(dir, fmt.Sprintf("take%02d.wav", v))
			if err := os.WriteFile(p, buf, 0o644); err != nil {
				tb.Fatal(err)
			}
		}
	}

	list := func(string) ([]scan.Entry, error) {
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

func BenchmarkSteadyStatePass(b *testing.B) {
	root, list := buildCatalogue(b, 50, 10, 64*1024)

	a := &fakeAPI{holds: map[string]bool{}}
	s, c := newSyncer(&testing.T{}, root, list, a)

	if _, err := s.Once(context.Background()); err != nil {
		b.Fatal(err)
	}
	c.add(watch.DefaultQuiet * 2)
	if _, err := s.Once(context.Background()); err != nil {
		b.Fatal(err)
	}

	out, err := s.Once(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	if out.Skipped != 500 {
		b.Fatalf("setup wrong: %d skipped, want 500 (%+v)", out.Skipped, out)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.add(time.Minute)
		if _, err := s.Once(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
