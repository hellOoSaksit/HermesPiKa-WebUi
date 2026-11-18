// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package team

// Devices: how a person's own hpk joins a team server (docs/plans/connected-mode.md §12).
//
// An owner makes a one-time invite code for a member (expires in 15 minutes).
// The person's hpk exchanges it for a device token "hpkd_…". One person may
// have many devices; each has its own token and can be revoked alone. Like
// personal tokens, codes and device tokens are stored as SHA-256 only, in
// <hermes home>/hpk/devices.json (0600).

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// InviteTTL is how long an invite code works.
const InviteTTL = 15 * time.Minute

// DevicePrefix starts every device token.
const DevicePrefix = "hpkd_"

// Invite is a one-time code waiting to be used.
type Invite struct {
	CodeHash string    `json:"code_sha256"`
	Member   string    `json:"member"`
	By       string    `json:"by"`
	Expires  time.Time `json:"expires"`
}

// Device is one machine a person joined from.
type Device struct {
	ID        string    `json:"id"`
	Member    string    `json:"member"`
	Name      string    `json:"name"`
	TokenHash string    `json:"token_sha256"`
	Added     time.Time `json:"added"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
	LastIP    string    `json:"last_ip,omitempty"`
}

// Public is a device without its token hash.
type PublicDevice struct {
	ID       string    `json:"id"`
	Member   string    `json:"member"`
	Name     string    `json:"name"`
	Added    time.Time `json:"added"`
	LastSeen time.Time `json:"lastSeen,omitempty"`
	LastIP   string    `json:"lastIP,omitempty"`
}

// Devices is the store. Safe for concurrent use.
type Devices struct {
	mu      sync.Mutex
	file    string
	Invites []Invite `json:"invites"`
	List    []Device `json:"devices"`
	saved   time.Time
}

// DevicesFile is where devices and invites live.
func DevicesFile(home string) string { return filepath.Join(home, "hpk", "devices.json") }

// LoadDevices reads devices.json; a missing file is an empty store.
func LoadDevices(home string) (*Devices, error) {
	d := &Devices{file: DevicesFile(home)}
	raw, err := os.ReadFile(d.file)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, d); err != nil {
		return nil, fmt.Errorf("devices.json: %w", err)
	}
	return d, nil
}

func (d *Devices) saveLocked() error {
	if d.file == "" {
		return errors.New("devices.json could not be read; fix or remove it")
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.file), 0o755); err != nil {
		return err
	}
	tmp := d.file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	d.saved = time.Now()
	return os.Rename(tmp, d.file)
}

// Code alphabet: no 0/O, 1/I/L, so a code read aloud is typed right.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func newCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, 11)
	for i, x := range b {
		if i == 5 {
			out = append(out, '-')
		}
		out = append(out, codeAlphabet[int(x)%len(codeAlphabet)])
	}
	return string(out), nil
}

// NormalizeCode makes "abcde fghjk" and "ABCDE-FGHJK" the same.
func NormalizeCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if strings.ContainsRune(codeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) == 10 {
		return s[:5] + "-" + s[5:]
	}
	return s
}

// NewInvite makes a one-time code for a member of c. It is returned once.
func (d *Devices) NewInvite(c *Config, member, by string, now time.Time) (string, time.Time, error) {
	m, ok := c.Find(member)
	if !ok {
		return "", time.Time{}, fmt.Errorf("%s is not in the team; add them first", member)
	}
	code, err := newCode()
	if err != nil {
		return "", time.Time{}, err
	}
	exp := now.Add(InviteTTL).UTC()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dropExpiredLocked(now)
	d.Invites = append(d.Invites, Invite{CodeHash: HashToken(code), Member: m.ID, By: by, Expires: exp})
	return code, exp, d.saveLocked()
}

func (d *Devices) dropExpiredLocked(now time.Time) {
	keep := d.Invites[:0]
	for _, in := range d.Invites {
		if now.Before(in.Expires) {
			keep = append(keep, in)
		}
	}
	d.Invites = keep
}

var errBadCode = errors.New("that invite code is not valid or has expired; ask an owner for a new one")

// Join uses an invite code (once) and returns a new device and its token.
func (d *Devices) Join(c *Config, code, deviceName string, now time.Time) (Device, string, error) {
	code = NormalizeCode(code)
	if len(code) != 11 {
		return Device{}, "", errBadCode
	}
	name := cleanDeviceName(deviceName)
	h := []byte(HashToken(code))
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dropExpiredLocked(now)
	at := -1
	for i, in := range d.Invites {
		if subtle.ConstantTimeCompare(h, []byte(in.CodeHash)) == 1 {
			at = i
		}
	}
	if at < 0 {
		return Device{}, "", errBadCode
	}
	in := d.Invites[at]
	d.Invites = append(d.Invites[:at], d.Invites[at+1:]...) // one use, even if the rest fails
	m, ok := c.Find(in.Member)
	if !ok {
		_ = d.saveLocked()
		return Device{}, "", errors.New(in.Member + " is no longer in the team")
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return Device{}, "", err
	}
	token := DevicePrefix + hex.EncodeToString(b)
	idb := make([]byte, 6)
	_, _ = rand.Read(idb)
	dev := Device{ID: "dev_" + hex.EncodeToString(idb), Member: m.ID, Name: name, TokenHash: HashToken(token), Added: now.UTC()}
	d.List = append(d.List, dev)
	return dev, token, d.saveLocked()
}

func cleanDeviceName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	if s == "" {
		s = "device"
	}
	return s
}

// ByToken finds the device a token belongs to and the member it acts for. A
// device of someone no longer in the team does not work. Last seen is kept in
// memory and written at most once a minute.
func (d *Devices) ByToken(c *Config, token, ip string, now time.Time) (*Person, Device, bool) {
	if !strings.HasPrefix(token, DevicePrefix) {
		return nil, Device{}, false
	}
	h := []byte(HashToken(token))
	d.mu.Lock()
	defer d.mu.Unlock()
	at := -1
	for i := range d.List {
		if subtle.ConstantTimeCompare(h, []byte(d.List[i].TokenHash)) == 1 {
			at = i
		}
	}
	if at < 0 {
		return nil, Device{}, false
	}
	m, ok := c.Find(d.List[at].Member)
	if !ok {
		return nil, Device{}, false
	}
	first := d.List[at].LastSeen.IsZero()
	d.List[at].LastSeen, d.List[at].LastIP = now.UTC(), ip
	// the first use is written at once, so `hpk team devices` (another process) sees it
	if first || now.Sub(d.saved) > time.Minute {
		_ = d.saveLocked()
	}
	return m, d.List[at], true
}

// For lists devices without token hashes: one member's, or everyone's ("").
func (d *Devices) For(member string) []PublicDevice {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []PublicDevice{}
	for _, x := range d.List {
		if member == "" || strings.EqualFold(x.Member, member) {
			out = append(out, PublicDevice{ID: x.ID, Member: x.Member, Name: x.Name, Added: x.Added, LastSeen: x.LastSeen, LastIP: x.LastIP})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Member != out[j].Member {
			return out[i].Member < out[j].Member
		}
		return out[i].Added.Before(out[j].Added)
	})
	return out
}

// Revoke removes a device. With member set, only that member's device.
func (d *Devices) Revoke(id, member string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, x := range d.List {
		if x.ID == id && (member == "" || strings.EqualFold(x.Member, member)) {
			d.List = append(d.List[:i], d.List[i+1:]...)
			return d.saveLocked()
		}
	}
	return fmt.Errorf("no device %q", id)
}

// RevokeMember removes every device and every pending invite of a member (they
// left the team, or their access was reset). Re-adding the same id later does
// not bring any of them back.
func (d *Devices) RevokeMember(member string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	keep := d.List[:0]
	for _, x := range d.List {
		if !strings.EqualFold(x.Member, member) {
			keep = append(keep, x)
		}
	}
	d.List = keep
	inv := d.Invites[:0]
	for _, x := range d.Invites {
		if !strings.EqualFold(x.Member, member) {
			inv = append(inv, x)
		}
	}
	d.Invites = inv
	return d.saveLocked()
}

// IPAllowed reports whether ip may reach the server. An empty allowlist allows
// everyone; loopback is always allowed (the server's own machine).
func (c *Config) IPAllowed(ip net.IP) bool {
	if len(c.AllowIPs) == 0 {
		return true
	}
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, a := range c.AllowIPs {
		if _, n, err := net.ParseCIDR(a); err == nil && n.Contains(ip) {
			return true
		}
		if x := net.ParseIP(a); x != nil && x.Equal(ip) {
			return true
		}
	}
	return false
}
