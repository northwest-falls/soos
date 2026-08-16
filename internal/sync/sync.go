package sync

import (
	"context"
	"path/filepath"
	"time"

	"github.com/northwest-falls/soos/internal/api"
	"github.com/northwest-falls/soos/internal/contenthash"
	"github.com/northwest-falls/soos/internal/index"
	"github.com/northwest-falls/soos/internal/scan"
	"github.com/northwest-falls/soos/internal/watch"
)

type Uploader interface {
	Have(ctx context.Context, hashes []string) (map[string]bool, error)
	UploadFile(ctx context.Context, path string, req api.InitRequest,
		onProgress func(sent, total int64)) (*api.InitResult, error)
}

type Syncer struct {
	Root    string
	Opts    scan.Options
	Index   *index.Index
	Settler *watch.Settler
	API     Uploader

	List func(root string) ([]scan.Entry, error)

	Now func() time.Time
}

type Outcome struct {
	Uploaded int

	Deduped int

	Skipped int

	Waiting int
	Failed  int

	Paused bool

	Stopped bool

	Reason error

	Ignored map[string]int
	Errors  []error
}

const (
	MinInterval = 15 * time.Second
	MaxInterval = 10 * time.Minute
)

func (o *Outcome) Busy() bool {
	return o.Uploaded > 0 || o.Deduped > 0 || o.Waiting > 0 || o.Failed > 0
}

func (o *Outcome) NextCheck(current time.Duration) time.Duration {
	switch {
	case o.Stopped:

		return 0
	case o.Paused:
		return 30 * time.Minute
	case o.Busy():
		return MinInterval
	}

	next := current * 2
	if next < MinInterval {
		next = MinInterval
	}
	if next > MaxInterval {
		next = MaxInterval
	}

	return next
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

type pending struct {
	cand scan.Candidate
	hash string
	size int64
	mod  time.Time
}

func (s *Syncer) Once(ctx context.Context) (*Outcome, error) {
	out := &Outcome{Ignored: map[string]int{}}

	res, err := scan.Walk(s.Root, s.Opts, s.List)
	if err != nil {
		return nil, err
	}
	out.Ignored = res.Skipped

	var todo []pending

	for _, c := range res.Candidates {
		if err := ctx.Err(); err != nil {
			return out, err
		}

		// Index first. The settler forgets a file once it settles, so asking it
		// first makes every known file a first sighting again on every pass.
		if e, ok := s.Index.Lookup(c.Path, c.Size, c.Mod); ok && e.Uploaded {
			out.Skipped++
			continue
		}

		state, info, err := s.Settler.Check(c.Path)
		if err != nil {
			out.Failed++
			out.Errors = append(out.Errors, err)
			continue
		}
		if state != watch.Ready {

			if state != watch.Vanished {
				out.Waiting++
			}
			continue
		}

		hash, err := contenthash.File(c.Path)
		if err != nil {
			out.Failed++
			out.Errors = append(out.Errors, err)
			continue
		}

		s.Index.Put(c.Path, index.Entry{
			Size: info.Size(),
			Mod:  info.ModTime(),
			Hash: hash,
		})

		todo = append(todo, pending{cand: c, hash: hash, size: info.Size(), mod: info.ModTime()})
	}

	if len(todo) == 0 {
		return out, nil
	}

	hashes := make([]string, 0, len(todo))
	seen := make(map[string]bool, len(todo))
	for _, p := range todo {
		if !seen[p.hash] {
			seen[p.hash] = true
			hashes = append(hashes, p.hash)
		}
	}

	have, err := s.API.Have(ctx, hashes)
	if err != nil {

		switch api.Classify(err) {
		case api.Stop:
			out.Stopped, out.Reason = true, err
		case api.Pause:
			out.Paused, out.Reason = true, err
		}
		return out, err
	}

	for _, p := range todo {
		if err := ctx.Err(); err != nil {
			return out, err
		}

		if have[p.hash] {

			s.Index.MarkUploaded(p.cand.Path)
			out.Deduped++
			continue
		}

		if err := s.upload(ctx, p); err != nil {
			out.Failed++
			out.Errors = append(out.Errors, err)

			switch api.Classify(err) {
			case api.Stop:
				out.Stopped = true
				out.Reason = err
				return out, err
			case api.Pause:
				out.Paused = true
				out.Reason = err
				return out, err
			}

			continue
		}

		out.Uploaded++
	}

	return out, nil
}

func (s *Syncer) upload(ctx context.Context, p pending) error {
	req := api.InitRequest{
		// The base name, not the whole path. Sending the absolute path made the
		// vault title read C:\Users\...\songs\Project 23.
		Filename:    filepath.Base(p.cand.Path),
		ContentHash: p.hash,
		Kind:        kindFor(p.cand.Kind),
	}

	// A file inside a subfolder makes that folder the track, so its name is the
	// title. A loose file in the watched folder has no folder to name it, so the
	// title is left for the server to take from the file.
	if p.cand.Folder != s.Root {
		req.Title = p.cand.Title
	}

	if b, ok := s.Index.TrackFor(p.cand.Folder); ok {
		req.TrackID = b.TrackID
	}

	res, err := s.API.UploadFile(ctx, p.cand.Path, req, nil)
	if err != nil {
		return err
	}

	// Only after complete returned. Marking earlier means a failed upload is
	// never retried.
	s.Index.MarkUploaded(p.cand.Path)

	if res != nil && res.Track.ID != "" {
		if _, bound := s.Index.TrackFor(p.cand.Folder); !bound {
			s.Index.BindTrack(p.cand.Folder, res.Track.ID, s.now())
		}
	}

	return nil
}

func kindFor(k scan.Kind) string {
	switch k {
	case scan.Stem:
		return "stem"
	case scan.Project:
		return "project"
	case scan.Artwork:
		return "artwork"
	default:
		return "master"
	}
}
