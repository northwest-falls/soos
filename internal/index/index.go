package index

import (
	"encoding/gob"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const format = 2

type Entry struct {
	Size int64
	Mod  time.Time
	Hash string

	Uploaded bool
}

type Binding struct {
	TrackID string
	BoundAt time.Time
}

type snapshot struct {
	Format   int
	Entries  map[string]Entry
	Bindings map[string]Binding
}

type Index struct {
	mu       sync.RWMutex
	path     string
	entries  map[string]Entry
	bindings map[string]Binding
	dirty    bool
}

func Open(path string) (*Index, bool, error) {
	ix := &Index{
		path:     path,
		entries:  make(map[string]Entry),
		bindings: make(map[string]Binding),
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ix, false, nil
		}
		return ix, true, nil
	}
	defer f.Close()

	var snap snapshot
	if err := gob.NewDecoder(f).Decode(&snap); err != nil && !errors.Is(err, io.EOF) {
		return ix, true, nil
	}

	if snap.Format != format || snap.Entries == nil {
		return ix, snap.Format != 0, nil
	}

	ix.entries = snap.Entries
	if snap.Bindings != nil {
		ix.bindings = snap.Bindings
	}

	return ix, false, nil
}

func (ix *Index) Lookup(path string, size int64, mod time.Time) (Entry, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	e, ok := ix.entries[path]
	if !ok {
		return Entry{}, false
	}

	if e.Size != size || !e.Mod.Equal(mod) {
		return Entry{}, false
	}

	return e, true
}

func (ix *Index) Put(path string, e Entry) {
	ix.mu.Lock()
	ix.entries[path] = e
	ix.dirty = true
	ix.mu.Unlock()
}

func (ix *Index) MarkUploaded(path string) {
	ix.mu.Lock()
	if e, ok := ix.entries[path]; ok {
		e.Uploaded = true
		ix.entries[path] = e
		ix.dirty = true
	}
	ix.mu.Unlock()
}

func (ix *Index) Forget(path string) {
	ix.mu.Lock()
	if _, ok := ix.entries[path]; ok {
		delete(ix.entries, path)
		ix.dirty = true
	}
	ix.mu.Unlock()
}

func (ix *Index) KnownAt(hash string) []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	var out []string
	for p, e := range ix.entries {
		if e.Hash == hash {
			out = append(out, p)
		}
	}

	return out
}

func (ix *Index) TrackFor(folder string) (Binding, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	b, ok := ix.bindings[folder]

	return b, ok
}

func (ix *Index) BindTrack(folder, trackID string, at time.Time) {
	ix.mu.Lock()
	ix.bindings[folder] = Binding{TrackID: trackID, BoundAt: at}
	ix.dirty = true
	ix.mu.Unlock()
}

func (ix *Index) Unbind(folder string) {
	ix.mu.Lock()
	if _, ok := ix.bindings[folder]; ok {
		delete(ix.bindings, folder)
		ix.dirty = true
	}
	ix.mu.Unlock()
}

func (ix *Index) FoldersForTrack(trackID string) []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	var out []string
	for folder, b := range ix.bindings {
		if b.TrackID == trackID {
			out = append(out, folder)
		}
	}

	return out
}

func (ix *Index) Rebind(oldFolder, newFolder string) bool {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	b, ok := ix.bindings[oldFolder]
	if !ok {
		return false
	}

	delete(ix.bindings, oldFolder)
	ix.bindings[newFolder] = b
	ix.dirty = true

	return true
}

func (ix *Index) Bindings() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.bindings)
}

func (ix *Index) Prune(exists func(string) bool) int {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	var dropped int
	for p := range ix.entries {
		if !exists(p) {
			delete(ix.entries, p)
			dropped++
		}
	}

	if dropped > 0 {
		ix.dirty = true
	}

	return dropped
}

func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.entries)
}

func (ix *Index) Dirty() bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.dirty
}

func (ix *Index) Save() error {
	ix.mu.Lock()
	snap := snapshot{
		Format:   format,
		Entries:  make(map[string]Entry, len(ix.entries)),
		Bindings: make(map[string]Binding, len(ix.bindings)),
	}
	for k, v := range ix.entries {
		snap.Entries[k] = v
	}
	for k, v := range ix.bindings {
		snap.Bindings[k] = v
	}
	ix.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(ix.path), 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(ix.path), ".index-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	if err := gob.NewEncoder(tmp).Encode(snap); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, ix.path); err != nil {
		return err
	}

	ix.mu.Lock()
	ix.dirty = false
	ix.mu.Unlock()

	return nil
}
