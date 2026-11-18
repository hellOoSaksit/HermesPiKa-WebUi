// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package vault keeps a person's secrets (passwords, logins, tokens, .env
// values) for the built-in browser and for .env files, so the AI never sees
// them. The AI only ever handles names: {{secret:github}}.
//
//   - Storage: <home>/hpk/vault/vault.bin, AES-256-GCM. The 32-byte key is
//     protected by the operating system for this OS account (Windows DPAPI,
//     macOS Keychain, Linux Secret Service; a 0600 key file as the last resort,
//     reported as weaker).
//   - Values leave the vault only to be typed into a page on an allowed site, to
//     be written into a .env file a person asked for, or to be shown to the
//     person in the Console after a confirmation. Never to a model, a log, the
//     wiki or a recording.
//   - Scrub replaces any secret value found in text with its {{secret:name}}.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one secret.
type Entry struct {
	Name     string    `json:"name"`              // {{secret:name}}
	Kind     string    `json:"kind"`              // password | login | token | env | note
	User     string    `json:"user,omitempty"`    // login: the user name (also secret)
	Value    string    `json:"value"`             // never sent to the Console list
	Domains  []string  `json:"domains,omitempty"` // sites it may be filled on (example.com covers its subdomains)
	EnvKey   string    `json:"envKey,omitempty"`  // env: the variable name
	Project  string    `json:"project,omitempty"` // env: which project's .env
	EnvFile  string    `json:"envFile,omitempty"` // env: path in the project, default .env
	Note     string    `json:"note,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	LastUsed time.Time `json:"lastUsed,omitempty"`
	Uses     int       `json:"uses,omitempty"`
}

// Public is an entry without its value, for lists.
type Public struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	HasUser  bool      `json:"hasUser"`
	Domains  []string  `json:"domains"`
	EnvKey   string    `json:"envKey,omitempty"`
	Project  string    `json:"project,omitempty"`
	EnvFile  string    `json:"envFile,omitempty"`
	Note     string    `json:"note,omitempty"`
	Length   int       `json:"length"` // shown as dots
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	LastUsed time.Time `json:"lastUsed,omitempty"`
	Uses     int       `json:"uses"`
}

var (
	nameRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,47}$`)
	envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	kinds    = map[string]bool{"password": true, "login": true, "token": true, "env": true, "note": true}
	refRE    = regexp.MustCompile(`\{\{secret:([a-z0-9][a-z0-9_.-]{0,47}?)(?:\.(user|value))?\}\}`)
)

// Vault is the open store. Safe for concurrent use.
type Vault struct {
	mu      sync.Mutex
	dir     string
	key     []byte
	entries map[string]*Entry
	pin     *pinFile  // the PIN verifier, sealed with the entries
	fails   int       // wrong PINs in a row (sealed too, so a restart does not reset it)
	lockTo  time.Time // no PIN tries before this
	Keeper  string    // how the key is protected (dpapi, keychain, secret-service, file)
}

// stored is what vault.bin holds (sealed). Version 1 files were a bare entry list.
type stored struct {
	V       int       `json:"v"`
	Entries []*Entry  `json:"entries"`
	PIN     *pinFile  `json:"pin,omitempty"`
	Fails   int       `json:"fails,omitempty"`
	LockTo  time.Time `json:"lockTo,omitempty"`
}

func (v *Vault) path() string { return filepath.Join(v.dir, "vault.bin") }

// Open opens (or creates) the vault in <home>/hpk/vault.
func Open(home string) (*Vault, error) {
	dir := filepath.Join(home, "hpk", "vault")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, keeper, err := loadKey(dir)
	if err != nil {
		return nil, fmt.Errorf("vault key: %w", err)
	}
	v := &Vault{dir: dir, key: key, entries: map[string]*Entry{}, Keeper: keeper}
	raw, err := os.ReadFile(v.path())
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := v.open(raw)
	if err != nil {
		return nil, errors.New("vault: cannot decrypt (another OS account or a changed key)")
	}
	var st stored
	if len(plain) > 0 && plain[0] == '[' {
		if err := json.Unmarshal(plain, &st.Entries); err != nil {
			return nil, err
		}
	} else if err := json.Unmarshal(plain, &st); err != nil {
		return nil, err
	}
	for _, e := range st.Entries {
		v.entries[e.Name] = e
	}
	v.pin, v.fails, v.lockTo = st.PIN, st.Fails, st.LockTo
	// a version-1 PIN file next to the vault: move it inside, then remove it
	if v.pin == nil {
		if raw, err := os.ReadFile(filepath.Join(dir, "pin.json")); err == nil {
			var pf pinFile
			if json.Unmarshal(raw, &pf) == nil && pf.Iter >= 100_000 && len(pf.Hash) == 32 {
				v.pin = &pf
				v.mu.Lock()
				err := v.saveLocked()
				v.mu.Unlock()
				if err == nil {
					os.Remove(filepath.Join(dir, "pin.json"))
				}
			}
		}
	}
	return v, nil
}

func (v *Vault) seal(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append([]byte("HPKV1"), gcm.Seal(nonce, nonce, plain, []byte("hpk-vault"))...), nil
}

func (v *Vault) open(raw []byte) ([]byte, error) {
	raw, ok := cutPrefix(raw, "HPKV1")
	if !ok {
		return nil, errors.New("not a vault file")
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("short")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte("hpk-vault"))
}

func cutPrefix(b []byte, p string) ([]byte, bool) {
	if len(b) < len(p) || string(b[:len(p)]) != p {
		return b, false
	}
	return b[len(p):], true
}

func (v *Vault) saveLocked() error {
	list := make([]*Entry, 0, len(v.entries))
	for _, e := range v.entries {
		list = append(list, e)
	}
	plain, _ := json.Marshal(stored{V: 2, Entries: list, PIN: v.pin, Fails: v.fails, LockTo: v.lockTo})
	sealed, err := v.seal(plain)
	if err != nil {
		return err
	}
	tmp := v.path() + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path())
}

func public(e *Entry) Public {
	d := e.Domains
	if d == nil {
		d = []string{}
	}
	return Public{Name: e.Name, Kind: e.Kind, HasUser: e.User != "", Domains: d, EnvKey: e.EnvKey, Project: e.Project, EnvFile: e.EnvFile,
		Note: e.Note, Length: len([]rune(e.Value)), Created: e.Created, Updated: e.Updated, LastUsed: e.LastUsed, Uses: e.Uses}
}

// System secrets are hpk's own (the team-server device token): kept in the
// vault for its OS protection, but not listed, not named to the AI, not filled
// into pages, not shown and not deleted from the Console. Names start "hpk.".
const systemKind = "system"

// SystemPrefix starts the names of hpk's own secrets.
const SystemPrefix = "hpk."

// PutSystem stores one of hpk's own secrets.
func (v *Vault) PutSystem(name, value string) error {
	if !strings.HasPrefix(name, SystemPrefix) || !nameRE.MatchString(name) {
		return fmt.Errorf("%q is not a system secret name", name)
	}
	if value == "" || strings.ContainsAny(value, "\x00") || len(value) > 64<<10 {
		return errors.New("bad value")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now().UTC()
	e := &Entry{Name: name, Kind: systemKind, Value: value, Created: now, Updated: now}
	if old, ok := v.entries[name]; ok {
		e.Created = old.Created
	}
	v.entries[name] = e
	return v.saveLocked()
}

// System reads one of hpk's own secrets ("" if there is none).
func (v *Vault) System(name string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if e, ok := v.entries[name]; ok && e.Kind == systemKind {
		return e.Value
	}
	return ""
}

// DeleteSystem removes one of hpk's own secrets (no error if missing).
func (v *Vault) DeleteSystem(name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if e, ok := v.entries[name]; !ok || e.Kind != systemKind {
		return nil
	}
	delete(v.entries, name)
	return v.saveLocked()
}

func (v *Vault) isSystem(name string) bool {
	e, ok := v.entries[name]
	return ok && e.Kind == systemKind
}

// List returns every entry without values, by name (not hpk's own).
func (v *Vault) List() []Public {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]Public, 0, len(v.entries))
	for _, e := range v.entries {
		if e.Kind == systemKind {
			continue
		}
		out = append(out, public(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names lists the names the agent may refer to, with where each may be used.
func (v *Vault) Names() []map[string]any {
	out := []map[string]any{}
	for _, p := range v.List() {
		out = append(out, map[string]any{"name": p.Name, "kind": p.Kind, "domains": p.Domains, "hasUser": p.HasUser})
	}
	return out
}

// Put adds or replaces an entry. An empty Value on an existing entry keeps the old value.
func (v *Vault) Put(e Entry) error {
	e.Name = strings.ToLower(strings.TrimSpace(e.Name))
	if !nameRE.MatchString(e.Name) {
		return fmt.Errorf("%q: use a short name of a-z, 0-9, _ . - (e.g. github-login)", e.Name)
	}
	if strings.HasPrefix(e.Name, SystemPrefix) {
		return fmt.Errorf("names starting %q are kept for hpk itself", SystemPrefix)
	}
	if !kinds[e.Kind] {
		return fmt.Errorf("kind %q: use password, login, token, env or note", e.Kind)
	}
	if e.Kind == "env" {
		if !envKeyRE.MatchString(e.EnvKey) {
			return errors.New("an env secret needs a variable name like API_KEY")
		}
		if e.Project == "" {
			return errors.New("an env secret belongs to a project")
		}
		if e.EnvFile == "" {
			e.EnvFile = ".env"
		}
		if err := checkEnvFile(e.EnvFile); err != nil {
			return err
		}
	}
	if strings.ContainsAny(e.Value, "\x00") || len(e.Value) > 64<<10 {
		return errors.New("value too long")
	}
	clean := []string{}
	for _, d := range e.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if u, err := url.Parse(d); err == nil && u.Host != "" {
			d = u.Hostname()
		}
		d = strings.TrimSuffix(strings.TrimSuffix(d, "/"), ".")
		wild := strings.HasPrefix(d, "*.")
		d = strings.TrimPrefix(d, "*.")
		if d == "" {
			continue
		}
		if strings.ContainsAny(d, " /:@") {
			return fmt.Errorf("%q is not a site name (example.com)", d)
		}
		if wild {
			d = "*." + d
		}
		clean = append(clean, d)
	}
	e.Domains = clean
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now().UTC()
	if old, ok := v.entries[e.Name]; ok {
		if e.Value == "" {
			e.Value = old.Value
		}
		if e.User == "" && e.Kind == "login" {
			e.User = old.User
		}
		e.Created, e.LastUsed, e.Uses = old.Created, old.LastUsed, old.Uses
	} else {
		if v.pin == nil {
			return errors.New("set a vault PIN first (Settings > Vault)")
		}
		if e.Value == "" {
			return errors.New("give the secret's value")
		}
		e.Created = now
	}
	e.Updated = now
	v.entries[e.Name] = &e
	return v.saveLocked()
}

// Delete removes an entry.
func (v *Vault) Delete(name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.entries[name]; !ok || v.isSystem(name) {
		return fmt.Errorf("no secret %q", name)
	}
	delete(v.entries, name)
	return v.saveLocked()
}

// Reveal gives a value to the person (Console, after a confirmation). Never to the agent.
func (v *Vault) Reveal(name string) (user, value string, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.entries[name]
	if !ok || e.Kind == systemKind {
		return "", "", fmt.Errorf("no secret %q", name)
	}
	return e.User, e.Value, nil
}

// HostAllowed: may this secret be filled on a page at host?
//
// "example.com" matches that host only; "*.example.com" also matches its
// subdomains. Exact by default, so an allowed big site (google.com) does not
// open every page under it (docs.google.com forms, gists) to the secret.
func HostAllowed(domains []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, d := range domains {
		if base, wild := strings.CutPrefix(d, "*."); wild {
			if host == base || strings.HasSuffix(host, "."+base) {
				return true
			}
		} else if host == d {
			return true
		}
	}
	return false
}

// ForFill returns the text to type for a reference on a page at host.
// part: "value" (default) or "user". The use is counted.
func (v *Vault) ForFill(name, part, host string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.entries[name]
	if !ok || e.Kind == systemKind {
		return "", fmt.Errorf("no secret named %q (ask the person to add it in Console > Personal > Vault)", name)
	}
	if e.Kind == "env" || e.Kind == "note" {
		return "", fmt.Errorf("%q is not for typing into web pages", name)
	}
	if len(e.Domains) == 0 {
		return "", fmt.Errorf("%q has no allowed sites yet; the person must add %s to it in the Vault", name, host)
	}
	if !HostAllowed(e.Domains, host) {
		return "", fmt.Errorf("%q may only be used on %s, not on %s", name, strings.Join(e.Domains, ", "), host)
	}
	val := e.Value
	if part == "user" {
		if e.User == "" {
			return "", fmt.Errorf("%q has no user name", name)
		}
		val = e.User
	}
	e.Uses++
	e.LastUsed = time.Now().UTC()
	_ = v.saveLocked()
	return val, nil
}

// ParseRef reads "{{secret:name}}", "{{secret:name.user}}" or a bare name.
func ParseRef(s string) (name, part string, ok bool) {
	s = strings.TrimSpace(s)
	if m := refRE.FindStringSubmatch(s); m != nil && m[0] == s {
		part = m[2]
		if part == "" {
			part = "value"
		}
		return m[1], part, true
	}
	if n, p, found := strings.Cut(s, "."); found && nameRE.MatchString(n) && (p == "user" || p == "value") {
		return n, p, true
	}
	if nameRE.MatchString(s) {
		return s, "value", true
	}
	return "", "", false
}

// Scrub replaces every secret value (and login user name) that appears in text
// with its reference. Values shorter than 4 characters are not scrubbed (too
// many false hits); the vault form warns about them.
func (v *Vault) Scrub(text string) string {
	if text == "" {
		return text
	}
	v.mu.Lock()
	type pair struct{ val, ref string }
	var pairs []pair
	for _, e := range v.entries {
		if len(e.Value) >= 4 {
			pairs = append(pairs, pair{e.Value, "{{secret:" + e.Name + "}}"})
		}
		if len(e.User) >= 4 {
			pairs = append(pairs, pair{e.User, "{{secret:" + e.Name + ".user}}"})
		}
	}
	v.mu.Unlock()
	sort.Slice(pairs, func(i, j int) bool { return len(pairs[i].val) > len(pairs[j].val) })
	for _, p := range pairs {
		for _, form := range forms(p.val) {
			text = replaceFold(text, form, p.ref)
		}
	}
	return text
}

// forms are the ways a value shows up in page text, URLs and JSON: as is,
// JSON-escaped, URL-encoded (query, path, form), whitespace collapsed, and a
// long leading piece (pages and snapshots cut long values short).
func forms(v string) []string {
	out := []string{v}
	add := func(s string) {
		if len(s) >= 4 {
			for _, x := range out {
				if x == s {
					return
				}
			}
			out = append(out, s)
		}
	}
	if j, err := json.Marshal(v); err == nil {
		add(string(j[1 : len(j)-1]))
	}
	add(url.QueryEscape(v))
	add(url.PathEscape(v))
	add(strings.ReplaceAll(url.QueryEscape(v), "+", "%20"))
	add(strings.Join(strings.Fields(v), " "))
	if len(v) >= 16 {
		add(v[:12])
	}
	return out
}

// replaceFold replaces every occurrence of old in s, ignoring case.
func replaceFold(s, old, repl string) string {
	if old == "" {
		return s
	}
	ls, lo := strings.ToLower(s), strings.ToLower(old)
	if len(ls) != len(s) || len(lo) != len(old) { // case mapping changed lengths: plain replace
		return strings.ReplaceAll(s, old, repl)
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(ls[i:], lo)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		b.WriteString(s[i : i+j])
		b.WriteString(repl)
		i += j + len(old)
	}
}

// Has reports whether a secret exists.
func (v *Vault) Has(name string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.entries[name]
	return ok && e.Kind != systemKind
}
