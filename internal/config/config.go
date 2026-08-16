package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Folder struct {
	Path     string `json:"path"`
	Projects bool   `json:"projects"`
	Artwork  bool   `json:"artwork"`

	AllowCloudPlaceholders bool `json:"allow_cloud_placeholders"`
}

type Config struct {
	BaseURL  string   `json:"base_url"`
	DeviceID string   `json:"device_id"`
	Folders  []Folder `json:"folders"`
}

const (
	dirName    = "soos"
	fileName   = "config.json"
	indexName  = "index.gob"
	secretName = "credential"
)

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, dirName), nil
}

func IndexPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, indexName), nil
}

// UIAddrPath holds the address of the running interface so a second launch can
// open it rather than start a rival one. It carries the token, so it lives
// beside the credential and is written owner-only.
func UIAddrPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ui.addr"), nil
}

func Load() (*Config, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(filepath.Join(d, fileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{BaseURL: DefaultBaseURL}, nil
		}
		return nil, err
	}

	var c Config
	if err := json.Unmarshal(b, &c); err != nil {

		return nil, err
	}

	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}

	return &c, nil
}

const DefaultBaseURL = "https://api.eu.northwestfalls.com"

func Save(c *Config) error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}

	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return writeAtomic(filepath.Join(d, fileName), b, 0o600)
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()

	defer func() {
		tmp.Close()
		os.Remove(name)
	}()

	if err := tmp.Chmod(perm); err != nil && !errors.Is(err, os.ErrInvalid) {

		_ = err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(name, path)
}

func SaveToken(token string) error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}

	sealed, err := seal([]byte(token))
	if err != nil {
		return err
	}

	return writeAtomic(filepath.Join(d, secretName), sealed, 0o600)
}

func LoadToken() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}

	b, err := os.ReadFile(filepath.Join(d, secretName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	open, err := unseal(b)
	if err != nil {

		return "", nil
	}

	return string(open), nil
}

func ForgetToken() error {
	d, err := Dir()
	if err != nil {
		return err
	}

	err = os.Remove(filepath.Join(d, secretName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return err
}
