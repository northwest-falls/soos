package scan

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Kind int

const (
	Ignored Kind = iota
	Version
	Stem
	Project
	Artwork
)

func (k Kind) String() string {
	switch k {
	case Version:
		return "version"
	case Stem:
		return "stem"
	case Project:
		return "project"
	case Artwork:
		return "artwork"
	default:
		return "ignored"
	}
}

var audioExt = map[string]bool{
	".wav": true, ".aiff": true, ".aif": true, ".flac": true,
	".mp3": true, ".m4a": true, ".aac": true, ".ogg": true, ".opus": true,
}

var projectExt = map[string]bool{
	".flp": true, ".als": true, ".logicx": true, ".ptx": true, ".cpr": true,
	".rpp": true, ".song": true, ".bwproject": true, ".zip": true,
}

var artworkExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
}

var documentExt = map[string]bool{
	".pdf": true, ".doc": true, ".docx": true, ".txt": true, ".rtf": true,
	".xls": true, ".xlsx": true, ".csv": true, ".pages": true, ".key": true,
}

var videoExt = map[string]bool{
	".mp4": true, ".mov": true, ".avi": true, ".mkv": true, ".webm": true,
}

var noiseExt = map[string]bool{
	".asd": true, ".reapeaks": true, ".pkf": true, ".sfk": true, ".ovw": true,
	".tmp": true, ".temp": true, ".part": true, ".crdownload": true, ".download": true,
}

var noiseName = map[string]bool{
	".ds_store": true, "thumbs.db": true, "desktop.ini": true, ".localized": true,
}

type Candidate struct {
	Path string

	Folder string

	Title string
	Kind  Kind

	Reason string
	Size   int64
	Mod    time.Time
}

func Classify(root, path string, info fs.FileInfo, opts Options) Candidate {
	c := Candidate{Path: path, Size: info.Size(), Mod: info.ModTime()}

	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))

	if strings.HasPrefix(name, ".") || noiseName[name] || noiseExt[ext] {
		c.Reason = "noise"
		return c
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		c.Reason = "outside the watched folder"
		return c
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	depth := len(parts) - 1

	switch {
	case depth == 0:

		c.Folder = root
		c.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	default:
		c.Folder = filepath.Join(root, parts[0])
		c.Title = parts[0]
	}

	switch {
	case audioExt[ext]:
		if depth <= 1 {
			c.Kind = Version
		} else {

			c.Kind = Stem
		}

	case projectExt[ext]:
		if !opts.Projects {
			c.Reason = "session file, not switched on for this folder"
			return c
		}
		c.Kind = Project

	case artworkExt[ext]:
		if !opts.Artwork {
			c.Reason = "image, not switched on for this folder"
			return c
		}
		c.Kind = Artwork

	case documentExt[ext]:

		c.Reason = "document, never uploaded"

	case videoExt[ext]:
		c.Reason = "video, never uploaded"

	default:
		c.Reason = "not an audio file"
	}

	return c
}

type Options struct {
	Projects bool
	Artwork  bool
}

type Result struct {
	Candidates []Candidate

	Skipped map[string]int
}

func Walk(root string, opts Options, list func(string) ([]Entry, error)) (*Result, error) {
	res := &Result{Skipped: map[string]int{}}

	entries, err := list(root)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		c := Classify(root, e.Path(), e.Info(), opts)
		if c.Kind == Ignored {
			if c.Reason != "noise" {
				res.Skipped[c.Reason]++
			}
			continue
		}

		res.Candidates = append(res.Candidates, c)
	}

	sort.SliceStable(res.Candidates, func(i, j int) bool {
		a, b := res.Candidates[i], res.Candidates[j]
		if a.Folder != b.Folder {
			return a.Folder < b.Folder
		}
		if !a.Mod.Equal(b.Mod) {
			return a.Mod.Before(b.Mod)
		}

		return a.Path < b.Path
	})

	return res, nil
}

type Entry interface {
	Path() string
	Info() fs.FileInfo
	IsDir() bool
}
