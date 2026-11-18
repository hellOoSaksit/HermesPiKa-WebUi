// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package vault

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The key is protected with DPAPI for this Windows account.
func loadKey(dir string) ([]byte, string, error) {
	p := filepath.Join(dir, "key.dpapi")
	if blob, err := os.ReadFile(p); err == nil {
		key, err := dpapi(blob, false)
		if err != nil {
			return nil, "", err
		}
		return key, "dpapi", nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, "", err
	}
	blob, err := dpapi(key, true)
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(p, blob, 0o600); err != nil {
		return nil, "", err
	}
	return key, "dpapi", nil
}

func dpapi(in []byte, protect bool) ([]byte, error) {
	if len(in) == 0 {
		return nil, errors.New("empty")
	}
	src := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	ent := []byte("hpk-vault-key")
	entropy := windows.DataBlob{Size: uint32(len(ent)), Data: &ent[0]}
	var out windows.DataBlob
	var err error
	if protect {
		err = windows.CryptProtectData(&src, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&src, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
