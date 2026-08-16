//go:build darwin

package config

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

const (
	keychainService = "com.northwestfalls.soos"
	keychainAccount = "device-credential"
	marker          = "stored-in-keychain"
)

func seal(plain []byte) ([]byte, error) {

	cmd := exec.Command("security", "add-generic-password",
		"-a", keychainAccount,
		"-s", keychainService,
		"-w", string(plain),
		"-U")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, errors.New("keychain write failed: " + strings.TrimSpace(stderr.String()))
	}

	return []byte(marker), nil
}

func unseal(sealed []byte) ([]byte, error) {
	if string(bytes.TrimSpace(sealed)) != marker {
		return nil, errors.New("credential is not a keychain marker")
	}

	out, err := exec.Command("security", "find-generic-password",
		"-a", keychainAccount,
		"-s", keychainService,
		"-w").Output()
	if err != nil {
		return nil, errors.New("keychain read failed")
	}

	return bytes.TrimRight(out, "\r\n"), nil
}
