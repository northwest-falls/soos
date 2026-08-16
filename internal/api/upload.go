package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

const maxPartAttempts = 5

type InitRequest struct {
	Filename    string `json:"filename"`
	Title       string `json:"title,omitempty"`
	ByteSize    int64  `json:"byte_size"`
	ContentHash string `json:"content_hash"`
	Kind        string `json:"kind"`
	TrackID     string `json:"track_id,omitempty"`
	ReleaseID   string `json:"release_id,omitempty"`
}

type UploadPlan struct {
	AssetID   string `json:"asset_id"`
	Token     string `json:"token"`
	Multipart bool   `json:"multipart"`
	PartSize  int64  `json:"part_size"`
	PartCount int    `json:"part_count"`
	ExpiresAt string `json:"expires_at"`
}

type SimilarTrack struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type InitResult struct {
	Upload UploadPlan `json:"upload"`
	Track  struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"track"`
	Version struct {
		ID     string `json:"id"`
		Number int    `json:"number"`
	} `json:"version"`
	SimilarTrack *SimilarTrack `json:"similar_track,omitempty"`
}

func (c *Client) UploadInit(ctx context.Context, req InitRequest) (*InitResult, error) {
	if req.Kind == "" {
		req.Kind = "master"
	}

	var out InitResult
	if err := c.do(ctx, http.MethodPost, "/api/upload/init", req, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

func (c *Client) UploadComplete(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodPost, "/api/upload/complete",
		map[string]string{"token": token}, nil)
}

func (c *Client) uploadPart(ctx context.Context, token string, part int, sec *io.SectionReader) error {
	if _, err := sec.Seek(0, io.SeekStart); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/upload/part?token=%s&part=%d",
		c.BaseURL, urlQueryEscape(token), part)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, sec)
	if err != nil {
		return err
	}

	req.ContentLength = sec.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Length", strconv.FormatInt(sec.Size(), 10))
	req.Header.Set("User-Agent", "soos/"+c.Version)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		res.Body.Close()
	}()

	if res.StatusCode >= 400 {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&e)
		if e.Error == "" {
			e.Error = "part_failed"
		}
		return classifyResponse(res.StatusCode, e.Error, e.Message)
	}

	return nil
}

func (c *Client) UploadFile(
	ctx context.Context,
	path string,
	req InitRequest,
	onProgress func(sent, total int64),
) (*InitResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}

	req.ByteSize = st.Size()

	init, err := c.UploadInit(ctx, req)
	if err != nil {
		return nil, err
	}

	plan := init.Upload

	partSize := plan.PartSize
	partCount := plan.PartCount
	if !plan.Multipart || partSize <= 0 {
		partSize = st.Size()
		partCount = 1
		if partSize == 0 {
			partCount = 1
		}
	}

	var sent int64

	for part := 1; part <= partCount; part++ {
		off := int64(part-1) * partSize
		n := partSize
		if off+n > st.Size() {
			n = st.Size() - off
		}

		// SectionReader so a retry can rewind. A plain Reader would send nothing
		// the second time and store a truncated file.
		sec := io.NewSectionReader(f, off, n)

		if err := c.retryPart(ctx, plan.Token, part, sec); err != nil {
			return nil, fmt.Errorf("part %d of %d: %w", part, partCount, err)
		}

		sent += n
		if onProgress != nil {
			onProgress(sent, st.Size())
		}
	}

	if err := c.UploadComplete(ctx, plan.Token); err != nil {
		return nil, err
	}

	return init, nil
}

func (c *Client) retryPart(ctx context.Context, token string, part int, sec *io.SectionReader) error {
	wait := time.Second

	for attempt := 1; ; attempt++ {
		err := c.uploadPart(ctx, token, part, sec)
		if err == nil {
			return nil
		}

		if Classify(err) != Retry {
			return err
		}
		var apiErr *Error
		if errors.As(err, &apiErr) && !apiErr.Retryable() {
			return err
		}

		if attempt >= maxPartAttempts {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter(wait)):
		}

		if wait < 30*time.Second {
			wait *= 2
		}
	}
}

func urlQueryEscape(s string) string {
	const hex = "0123456789ABCDEF"

	var out []byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			out = append(out, ch)
			continue
		}
		out = append(out, '%', hex[ch>>4], hex[ch&0x0F])
	}

	return string(out)
}
