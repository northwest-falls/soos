//go:build !windows && !darwin

package config

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

const (
	secretSchema  = "com.northwestfalls.soos"
	secretKeyName = "device-credential"
	keyringMarker = "stored-in-keyring"
)

func haveSecretTool() bool {
	_, err := exec.LookPath("secret-tool")
	return err == nil
}

func seal(plain []byte) ([]byte, error) {
	if !haveSecretTool() {

		return plain, nil
	}

	cmd := exec.Command("secret-tool", "store",
		"--label=Northwest Falls (Soos)",
		"service", secretSchema,
		"account", secretKeyName)
	cmd.Stdin = bytes.NewReader(plain)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {

		return plain, nil
	}

	return []byte(keyringMarker), nil
}

func unseal(sealed []byte) ([]byte, error) {
	if string(bytes.TrimSpace(sealed)) != keyringMarker {

		if len(sealed) == 0 {
			return nil, errors.New("empty credential")
		}
		return bytes.TrimRight(sealed, "\r\n"), nil
	}

	out, err := exec.Command("secret-tool", "lookup",
		"service", secretSchema,
		"account", secretKeyName).Output()
	if err != nil {
		return nil, errors.New("keyring read failed: " + strings.TrimSpace(err.Error()))
	}

	return bytes.TrimRight(out, "\r\n"), nil
}
