package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/northwest-falls/soos/internal/api"
	"github.com/northwest-falls/soos/internal/config"
	"github.com/northwest-falls/soos/internal/contenthash"
	"github.com/northwest-falls/soos/internal/index"
)

const trackURL = "https://me.northwestfalls.com/#vault?track="
const vaultURL = "https://me.northwestfalls.com/#vault"

// Soos never creates the share link. That needs links:write, and a link is
// what makes a private master reachable by a stranger. He gets it ready; the
// person presses the button.
func cmdShare(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: soos share <file>")
	}

	c, _, err := client()
	if err != nil {
		return err
	}
	if c.Token == "" {
		return errors.New("not paired. Run: soos pair")
	}

	path, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}

	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return errors.New("that is a folder. Pick the file you want to share")
	}

	idxPath, err := config.IndexPath()
	if err != nil {
		return err
	}
	ix, _, err := index.Open(idxPath)
	if err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	folder := filepath.Dir(path)

	if e, ok := ix.Lookup(path, st.Size(), st.ModTime()); ok && e.Uploaded {
		if b, bound := ix.TrackFor(folder); bound {
			return openTrack(b.TrackID)
		}
	}

	fmt.Println("  Checking whether it is already in your vault.")

	hash, err := contenthash.File(path)
	if err != nil {
		return err
	}

	have, err := c.Have(ctx, []string{hash})
	if err != nil {
		return err
	}

	if have[hash] {
		ix.Put(path, index.Entry{Size: st.Size(), Mod: st.ModTime(), Hash: hash, Uploaded: true})
		_ = ix.Save()

		if b, bound := ix.TrackFor(folder); bound {
			return openTrack(b.TrackID)
		}

		fmt.Printf("\n  %s is already in your vault, but this computer did not\n", filepath.Base(path))
		fmt.Println("  upload it, so it cannot tell which track it became.")
		fmt.Println("  Opening your vault so you can pick it.")

		return browserOpen(vaultURL)
	}

	fmt.Printf("  Sending %s first.\n", filepath.Base(path))

	res, err := c.UploadFile(ctx, path, api.InitRequest{
		Filename:    path,
		ContentHash: hash,
		Kind:        "master",
		TrackID:     boundTrack(ix, folder),
	}, nil)
	if err != nil {
		return err
	}

	ix.Put(path, index.Entry{Size: st.Size(), Mod: st.ModTime(), Hash: hash, Uploaded: true})
	if res != nil && res.Track.ID != "" {
		if _, bound := ix.TrackFor(folder); !bound {
			ix.BindTrack(folder, res.Track.ID, nowFunc())
		}
	}
	_ = ix.Save()

	if res == nil || res.Track.ID == "" {
		return browserOpen(vaultURL)
	}

	return openTrack(res.Track.ID)
}

func boundTrack(ix *index.Index, folder string) string {
	if b, ok := ix.TrackFor(folder); ok {
		return b.TrackID
	}
	return ""
}

func openTrack(id string) error {
	fmt.Println("\n  Opening it in your vault. Press share there.")
	return browserOpen(trackURL + id)
}
