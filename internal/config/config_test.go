package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func isolate(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}

	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, dir) {
		t.Fatalf("isolation failed: config dir is %q, outside the temp dir %q. "+
			"This test would have written to the real user config.", got, dir)
	}

	return got
}

func TestMissingConfigGivesUsableDefaults(t *testing.T) {
	isolate(t)

	c, err := Load()
	if err != nil {
		t.Fatalf("a first run is not an error: %v", err)
	}
	if c.BaseURL != DefaultBaseURL {
		t.Fatalf("want the default base url, got %q", c.BaseURL)
	}
	if len(c.Folders) != 0 {
		t.Fatal("a fresh config should watch nothing")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	isolate(t)

	in := &Config{
		BaseURL:  DefaultBaseURL,
		DeviceID: "dev_123",
		Folders: []Folder{
			{Path: filepath.FromSlash("/music/Bounces"), Projects: true},
			{Path: filepath.FromSlash("/music/Archive"), AllowCloudPlaceholders: true},
		},
	}

	if err := Save(in); err != nil {
		t.Fatal(err)
	}

	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if out.DeviceID != "dev_123" || len(out.Folders) != 2 {
		t.Fatalf("config did not survive the round trip: %+v", out)
	}
	if !out.Folders[0].Projects || out.Folders[0].Artwork {
		t.Fatalf("per-folder options are wrong: %+v", out.Folders[0])
	}
	if !out.Folders[1].AllowCloudPlaceholders {
		t.Fatal("the cloud placeholder opt-in was lost, which would silently start downloading somebody's Dropbox")
	}
}

func TestTokenIsNeverWrittenIntoTheConfigFile(t *testing.T) {
	dir := isolate(t)

	const token = "nwa_thisisthesecretcredential"

	if err := SaveToken(token); err != nil {
		t.Fatal(err)
	}
	if err := Save(&Config{BaseURL: DefaultBaseURL, DeviceID: "dev_1"}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(body, []byte(token)) {
		t.Fatal("the credential ended up in config.json. It must never share a file with the settings.")
	}
	if bytes.Contains(body, []byte("nwa_")) {
		t.Fatal("something token-shaped is in config.json")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	isolate(t)

	if got, err := LoadToken(); err != nil || got != "" {
		t.Fatalf("an unpaired machine should report no token, got %q err %v", got, err)
	}

	const token = "nwa_abc123"
	if err := SaveToken(token); err != nil {
		t.Fatal(err)
	}

	got, err := LoadToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Fatalf("token did not survive: want %q, got %q", token, got)
	}
}

func TestForgetTokenIsIdempotent(t *testing.T) {
	isolate(t)

	if err := ForgetToken(); err != nil {
		t.Fatalf("forgetting nothing should be fine: %v", err)
	}

	if err := SaveToken("nwa_x"); err != nil {
		t.Fatal(err)
	}
	if err := ForgetToken(); err != nil {
		t.Fatal(err)
	}

	got, err := LoadToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("token survived being forgotten: %q", got)
	}
}

func TestSealedCredentialIsNotPlaintextOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI is Windows only; see seal_other.go for what the other platforms do")
	}

	dir := isolate(t)

	const token = "nwa_plaintextcanary"
	if err := SaveToken(token); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, secretName))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(raw, []byte(token)) {
		t.Fatal("the credential is sitting in the file in plain text; DPAPI did not seal it")
	}
	if len(raw) <= len(token) {
		t.Fatalf("sealed blob is %d bytes for a %d byte token, which is too small to be encrypted", len(raw), len(token))
	}
}

func TestUnreadableCredentialReadsAsUnpaired(t *testing.T) {
	dir := isolate(t)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, secretName), []byte("not a sealed blob"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadToken()
	if err != nil {
		t.Fatalf("an unreadable credential must not be a hard error: %v", err)
	}

	if runtime.GOOS == "windows" && got != "" {
		t.Fatalf("garbage unsealed into something: %q", got)
	}
}

func TestCorruptConfigIsAnErrorRatherThanASilentReset(t *testing.T) {
	dir := isolate(t)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("a corrupt config must be reported, not silently replaced with an empty one")
	}
}

func TestSaveLeavesNoTempFilesBehind(t *testing.T) {
	dir := isolate(t)

	for i := 0; i < 3; i++ {
		if err := Save(&Config{BaseURL: DefaultBaseURL, DeviceID: "d"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveToken("nwa_x"); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("atomic write left a temp file behind: %s", e.Name())
		}
	}
}

func TestIndexLivesOutsideTheWatchedFolders(t *testing.T) {
	dir := isolate(t)

	p, err := IndexPath()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(p, dir) {
		t.Fatalf("the index must live in the config directory, got %q", p)
	}
	if filepath.Base(p) != indexName {
		t.Fatalf("unexpected index name %q", filepath.Base(p))
	}
}
