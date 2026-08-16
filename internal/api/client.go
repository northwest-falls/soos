package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const MaxHashesPerCall = 500

type Client struct {
	BaseURL string
	Version string

	Token string

	HTTP *http.Client
}

func New(baseURL, version string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Version: version,
		HTTP: &http.Client{

			Timeout: 10 * time.Minute,
		},
	}
}

type Error struct {
	Status int
	Code   string
	Msg    string
}

func (e *Error) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("%s (%d): %s", e.Code, e.Status, e.Msg)
	}
	return fmt.Sprintf("%s (%d)", e.Code, e.Status)
}

func (e *Error) Retryable() bool {
	return e.Status == 429 || e.Status >= 500
}

var (
	ErrUnauthorized = errors.New("this device is no longer paired")

	ErrStorageFull = errors.New("the vault is full")

	ErrTrackLimit = errors.New("this plan is at its track limit")

	ErrReadOnly = errors.New("this account is read only")
)

type Disposition int

const (
	Retry Disposition = iota

	Pause

	Stop
)

func Classify(err error) Disposition {
	switch {
	case err == nil:
		return Retry
	case errors.Is(err, ErrUnauthorized):
		return Stop
	case errors.Is(err, ErrStorageFull),
		errors.Is(err, ErrTrackLimit),
		errors.Is(err, ErrReadOnly):
		return Pause
	}

	var apiErr *Error
	if errors.As(err, &apiErr) && !apiErr.Retryable() {

		return Retry
	}

	return Retry
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		enc, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(enc)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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

		return classifyResponse(res.StatusCode, e.Error, e.Message)
	}

	if out == nil {
		return nil
	}

	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
}

func classifyResponse(status int, code, msg string) error {
	base := &Error{Status: status, Code: code, Msg: msg}

	switch code {
	case "storage_full":
		return fmt.Errorf("%w: %s", ErrStorageFull, msg)
	case "track_limit":
		return fmt.Errorf("%w: %s", ErrTrackLimit, msg)
	case "account_read_only":
		return fmt.Errorf("%w: %s", ErrReadOnly, msg)
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ErrUnauthorized
	}

	return base
}

type PairStartResult struct {
	UserCode   string `json:"user_code"`
	PollSecret string `json:"poll_secret"`
	ExpiresAt  string `json:"expires_at"`
	ApproveURL string `json:"approve_url"`
}

// LocalRegister tells the account where this agent is reachable on loopback, so
// the vault page can play a track off this machine instead of the network.
func (c *Client) LocalRegister(ctx context.Context, url, token string) error {
	return c.do(ctx, http.MethodPost, "/api/agent/local",
		map[string]string{"url": url, "token": token}, nil)
}

func (c *Client) PairStart(ctx context.Context, deviceName, platform string) (*PairStartResult, error) {
	var out PairStartResult
	err := c.do(ctx, http.MethodPost, "/api/agent/pair/start", map[string]string{
		"device_name": deviceName,
		"platform":    platform,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type PairStatus string

const (
	PairPending  PairStatus = "pending"
	PairApproved PairStatus = "approved"
	PairExpired  PairStatus = "expired"
)

type PairClaimResult struct {
	Status   PairStatus `json:"status"`
	Token    string     `json:"token"`
	DeviceID string     `json:"device_id"`
}

func (c *Client) PairClaim(ctx context.Context, pollSecret string) (*PairClaimResult, error) {
	var out PairClaimResult
	err := c.do(ctx, http.MethodPost, "/api/agent/pair/claim", map[string]string{
		"poll_secret":   pollSecret,
		"agent_version": c.Version,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PairWait(ctx context.Context, pollSecret string) (*PairClaimResult, error) {
	wait := time.Second

	for {
		res, err := c.PairClaim(ctx, pollSecret)
		if err != nil {
			var apiErr *Error
			if errors.As(err, &apiErr) && !apiErr.Retryable() {
				return nil, err
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		} else if res.Status != PairPending {
			return res, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(jitter(wait)):
		}

		if wait < 15*time.Second {
			wait *= 2
		}
	}
}

func (c *Client) Have(ctx context.Context, hashes []string) (map[string]bool, error) {
	found := make(map[string]bool, len(hashes))

	for start := 0; start < len(hashes); start += MaxHashesPerCall {
		end := start + MaxHashesPerCall
		if end > len(hashes) {
			end = len(hashes)
		}

		var out struct {
			Have []string `json:"have"`
		}
		if err := c.do(ctx, http.MethodPost, "/api/agent/have", map[string]any{
			"hashes": hashes[start:end],
		}, &out); err != nil {
			return nil, err
		}

		for _, h := range out.Have {
			found[h] = true
		}
	}

	return found, nil
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(d))) + d/2
}
