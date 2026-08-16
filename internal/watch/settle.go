package watch

import (
	"os"
	"sync"
	"time"
)

const DefaultQuiet = 3 * time.Second

type observation struct {
	size int64
	mod  time.Time
	seen time.Time
}

type Settler struct {
	Quiet time.Duration
	Now   func() time.Time

	mu   sync.Mutex
	seen map[string]observation
}

func NewSettler() *Settler {
	return &Settler{
		Quiet: DefaultQuiet,
		Now:   time.Now,
		seen:  make(map[string]observation),
	}
}

type Result int

const (
	NotReady Result = iota
	Ready
	Vanished
	Placeholder
	Locked
)

func (r Result) String() string {
	switch r {
	case NotReady:
		return "not ready"
	case Ready:
		return "ready"
	case Vanished:
		return "vanished"
	case Placeholder:
		return "stored in the cloud, not on this machine"
	case Locked:
		return "still open in another program"
	default:
		return "unknown"
	}
}

func (s *Settler) Check(path string) (Result, os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.forget(path)
			return Vanished, nil, nil
		}
		return NotReady, nil, err
	}

	if info.IsDir() {
		return NotReady, nil, nil
	}

	if IsPlaceholder(path, info) {
		s.forget(path)
		return Placeholder, info, nil
	}

	now := s.Now()

	s.mu.Lock()
	prev, had := s.seen[path]
	changed := !had || prev.size != info.Size() || !prev.mod.Equal(info.ModTime())
	if changed {
		s.seen[path] = observation{size: info.Size(), mod: info.ModTime(), seen: now}
		s.mu.Unlock()
		return NotReady, info, nil
	}
	quiet := s.quiet()
	elapsed := now.Sub(prev.seen)
	s.mu.Unlock()

	if elapsed < quiet {
		return NotReady, info, nil
	}

	if !canOpenExclusive(path) {
		return Locked, info, nil
	}

	s.forget(path)

	return Ready, info, nil
}

func (s *Settler) Forget(path string) { s.forget(path) }

func (s *Settler) forget(path string) {
	s.mu.Lock()
	delete(s.seen, path)
	s.mu.Unlock()
}

func (s *Settler) quiet() time.Duration {
	if s.Quiet <= 0 {
		return DefaultQuiet
	}
	return s.Quiet
}

func (s *Settler) Tracking() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}
