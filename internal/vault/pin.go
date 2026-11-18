// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package vault

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// The vault PIN guards changes to existing secrets, deletes, .env writes and
// showing a value. The AI runs programs as the same OS account and could call
// hpk's local API; it cannot know a PIN the person types only in the Console.
//
// The verifier and the wrong-try counter are sealed inside vault.bin with the
// secrets, so they cannot be swapped or reset by editing a file, and a restart
// does not clear a lock-out. A PIN must exist before the first secret is
// stored, so nobody can "set the first PIN" over someone else's secrets.

type pinFile struct {
	Salt []byte `json:"salt"`
	Hash []byte `json:"hash"`
	Iter int    `json:"iter"`
}

const pinIter = 600_000

// HasPIN reports whether a PIN is set.
func (v *Vault) HasPIN() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.pin != nil
}

func hashPIN(pin string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, pin, salt, iter, 32)
}

// CheckStrength refuses PINs that are short or all one kind of character.
func CheckStrength(pin string) error {
	n := utf8.RuneCountInString(pin)
	if n < 8 || len(pin) > 128 {
		return errors.New("use a PIN of at least 8 characters")
	}
	if strings.Trim(pin, "0123456789") == "" && n < 10 {
		return errors.New("a PIN of digits only needs at least 10 digits; or mix in letters")
	}
	return nil
}

// SetPIN sets the PIN. Changing it needs the old one. Setting the first PIN is
// allowed only while the vault holds no person's secret.
func (v *Vault) SetPIN(old, pin string) error {
	if err := CheckStrength(pin); err != nil {
		return err
	}
	if v.HasPIN() {
		if err := v.CheckPIN(old); err != nil {
			return err
		}
	} else if v.personEntries() > 0 {
		return errors.New("this vault already holds secrets without a PIN; set it from the Console after confirming with the OS (hpk vault pin)")
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	h, err := hashPIN(pin, salt, pinIter)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.pin, v.fails, v.lockTo = &pinFile{Salt: salt, Hash: h, Iter: pinIter}, 0, time.Time{}
	return v.saveLocked()
}

// ForceSetPIN sets the first PIN of a vault that holds secrets from before PINs
// existed. Only the hpk command line calls it, in a terminal the person runs.
func (v *Vault) ForceSetPIN(pin string) error {
	if err := CheckStrength(pin); err != nil {
		return err
	}
	if v.HasPIN() {
		return errors.New("a PIN is already set; change it in the Console")
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	h, err := hashPIN(pin, salt, pinIter)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.pin = &pinFile{Salt: salt, Hash: h, Iter: pinIter}
	return v.saveLocked()
}

func (v *Vault) personEntries() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	n := 0
	for _, e := range v.entries {
		if e.Kind != systemKind {
			n++
		}
	}
	return n
}

// CheckPIN verifies the PIN. The try is counted before the slow hash, so
// parallel guesses cannot slip past the lock: after 5 wrong tries it locks for
// 5 minutes, then 10, 15 …
func (v *Vault) CheckPIN(pin string) error {
	v.mu.Lock()
	if v.pin == nil {
		v.mu.Unlock()
		return errors.New("set a vault PIN first")
	}
	if time.Now().Before(v.lockTo) {
		wait := time.Until(v.lockTo).Round(time.Second)
		v.mu.Unlock()
		return fmt.Errorf("too many wrong PINs; try again in %s", wait)
	}
	v.fails++
	if v.fails >= 5 {
		v.lockTo = time.Now().Add(time.Duration(v.fails-4) * 5 * time.Minute)
	}
	_ = v.saveLocked()
	p := *v.pin
	v.mu.Unlock()

	h, err := hashPIN(pin, p.Salt, p.Iter)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(h, p.Hash) != 1 {
		return errors.New("wrong PIN")
	}
	v.mu.Lock()
	v.fails, v.lockTo = 0, time.Time{}
	_ = v.saveLocked()
	v.mu.Unlock()
	return nil
}
