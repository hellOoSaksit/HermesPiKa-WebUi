// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The key goes to the OS secret store: macOS Keychain (security), or the
// Secret Service on Linux (secret-tool). Without either (a server, a
// container), a 0600 key file next to the vault, reported as "file".
func loadKey(dir string) ([]byte, string, error) {
	account := "hpk-vault:" + dir
	switch {
	case runtime.GOOS == "darwin" && has("security"):
		if out, err := exec.Command("security", "find-generic-password", "-s", "hpk-vault", "-a", account, "-w").Output(); err == nil {
			return decodeKey(out, "keychain")
		}
		key, hexKey, err := newKey()
		if err != nil {
			return nil, "", err
		}
		if err := exec.Command("security", "add-generic-password", "-U", "-s", "hpk-vault", "-a", account, "-w", hexKey).Run(); err == nil {
			return key, "keychain", nil
		}
	case runtime.GOOS == "linux" && has("secret-tool") && os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "":
		if out, err := exec.Command("secret-tool", "lookup", "service", "hpk-vault", "account", account).Output(); err == nil && len(bytes.TrimSpace(out)) > 0 {
			return decodeKey(out, "secret-service")
		}
		key, hexKey, err := newKey()
		if err != nil {
			return nil, "", err
		}
		cmd := exec.Command("secret-tool", "store", "--label=hpk vault", "service", "hpk-vault", "account", account)
		cmd.Stdin = strings.NewReader(hexKey)
		if cmd.Run() == nil {
			return key, "secret-service", nil
		}
	}
	p := filepath.Join(dir, "key")
	if raw, err := os.ReadFile(p); err == nil {
		return decodeKey(raw, "file")
	}
	key, hexKey, err := newKey()
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(p, []byte(hexKey), 0o600); err != nil {
		return nil, "", err
	}
	return key, "file", nil
}

func has(cmd string) bool { _, err := exec.LookPath(cmd); return err == nil }

func newKey() ([]byte, string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, "", err
	}
	return key, hex.EncodeToString(key), nil
}

func decodeKey(raw []byte, keeper string) ([]byte, string, error) {
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, "", errors.New("the stored vault key is damaged")
	}
	return key, keeper, nil
}
