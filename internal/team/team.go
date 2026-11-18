// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package team is hpk's team mode (docs/DESIGN.md §19): who may use the HPK
// Console and what each role may decide. It is written from scratch; no code
// comes from any Hermes web UI.
//
// Two ways to sign in:
//   - token: each member has a personal access token. hpk stores only its
//     SHA-256; the token is shown once, when it is made.
//   - header: a reverse proxy that already signs people in (oauth2-proxy,
//     Authelia, …) passes the person's id in a header. hpk trusts that header
//     only from the proxy addresses listed in trusted_proxies.
//
// The config is <hermes home>/hpk/team.yaml. Without it, hpk is in solo mode:
// loopback only, and the person running hpk serve is the owner.
package team

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Roles, from most to least power.
const (
	Owner      = "owner"      // settings, privacy, people, everything
	Maintainer = "maintainer" // + red lines, modes, core wiki pages
	Member     = "member"     // works in chats; approves general wiki drafts
	Viewer     = "viewer"     // reads only
)

var rank = map[string]int{Viewer: 0, Member: 1, Maintainer: 2, Owner: 3}

// ValidRole reports whether r is a role.
func ValidRole(r string) bool { _, ok := rank[r]; return ok }

// AtLeast reports whether role has at least the power of min.
func AtLeast(role, min string) bool { return rank[role] >= rank[min] }

// Actions the Console asks about, and the least role for each.
const (
	View         = "view"          // read pages
	Work         = "work"          // chats, sessions, board claims
	ApproveDraft = "approve_draft" // lessons, versions, glossary drafts
	ApproveCore  = "approve_core"  // decisions, architecture, people drafts
	RedLine      = "red_line"      // answer red-line approvals; change modes and red lines
	Admin        = "admin"         // privacy, settings, budgets, notices, jobs, projects, promotions
	People       = "people"        // add, change and remove members
)

var least = map[string]string{
	View: Viewer, Work: Member, ApproveDraft: Member, ApproveCore: Maintainer,
	RedLine: Maintainer, Admin: Owner, People: Owner,
}

// Can reports whether a role may take an action. Unknown actions are refused.
func Can(role, action string) bool {
	min, ok := least[action]
	return ok && AtLeast(role, min)
}

// Person is one member of the team.
type Person struct {
	ID        string   `yaml:"id"`                 // email or user name; from the proxy in header mode
	Name      string   `yaml:"name,omitempty"`     // shown in the Console
	Role      string   `yaml:"role"`               //
	Projects  []string `yaml:"projects,omitempty"` // empty = every project
	TokenHash string   `yaml:"token_sha256,omitempty"`
	// Money caps for what this person's chats cost, across projects (USD, 0 = none).
	BudgetDaily   float64   `yaml:"budget_daily_usd,omitempty"`
	BudgetMonthly float64   `yaml:"budget_monthly_usd,omitempty"`
	Added         time.Time `yaml:"added,omitempty"`
}

// Sees reports whether the member may see a project.
func (m Person) Sees(project string) bool {
	if len(m.Projects) == 0 {
		return true
	}
	for _, p := range m.Projects {
		if p == project {
			return true
		}
	}
	return false
}

// Label is the name to show.
func (m Person) Label() string {
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

// Config is team.yaml.
type Config struct {
	Enabled        bool     `yaml:"enabled"`
	Auth           string   `yaml:"auth"`                      // token | header
	Header         string   `yaml:"header,omitempty"`          // header mode: e.g. X-Forwarded-Email
	TrustedProxies []string `yaml:"trusted_proxies,omitempty"` // header mode: proxy IPs
	// Optional: only these addresses (IPs or CIDR ranges) may reach the server.
	// Loopback is always allowed. Empty = everyone.
	AllowIPs []string `yaml:"allow_ips,omitempty"`
	Members  []Person `yaml:"members"`

	path string
}

// File is where the config lives.
func File(home string) string { return filepath.Join(home, "hpk", "team.yaml") }

// Load reads team.yaml. A missing file is solo mode: (nil, nil).
func Load(home string) (*Config, error) {
	raw, err := os.ReadFile(File(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("team.yaml: %w", err)
	}
	c.path = File(home)
	return &c, c.check()
}

func (c *Config) check() error {
	if c.Auth == "" {
		c.Auth = "token"
	}
	if c.Auth != "token" && c.Auth != "header" {
		return fmt.Errorf("team.yaml: auth must be token or header, not %q", c.Auth)
	}
	if c.Auth == "header" && (c.Header == "" || len(c.TrustedProxies) == 0) {
		return errors.New("team.yaml: header mode needs header and trusted_proxies")
	}
	for _, a := range c.AllowIPs {
		if _, _, err := net.ParseCIDR(a); err != nil && net.ParseIP(a) == nil {
			return fmt.Errorf("team.yaml: allow_ips: %q is not an IP address or range", a)
		}
	}
	owners := 0
	seen := map[string]bool{}
	for _, m := range c.Members {
		if !idRE.MatchString(m.ID) {
			return fmt.Errorf("team.yaml: member id %q: use an email or a user name", m.ID)
		}
		if seen[strings.ToLower(m.ID)] {
			return fmt.Errorf("team.yaml: %s is listed twice", m.ID)
		}
		seen[strings.ToLower(m.ID)] = true
		if !ValidRole(m.Role) {
			return fmt.Errorf("team.yaml: %s has an unknown role %q", m.ID, m.Role)
		}
		if m.Role == Owner {
			owners++
		}
	}
	if c.Enabled && owners == 0 {
		return errors.New("team.yaml: a team needs at least one owner")
	}
	return nil
}

var idRE = regexp.MustCompile(`^[A-Za-z0-9._%+@-]{1,120}$`)

// Save writes team.yaml (0600: it holds token hashes).
func (c *Config) Save(home string) error {
	if err := c.check(); err != nil {
		return err
	}
	sort.SliceStable(c.Members, func(i, j int) bool { return rank[c.Members[i].Role] > rank[c.Members[j].Role] })
	raw, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	f := File(home)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, append([]byte("# hpk team mode (docs/team.md). Tokens are stored as SHA-256 only.\n"), raw...), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}

// Find returns a member by id (case-insensitive).
func (c *Config) Find(id string) (*Person, bool) {
	for i := range c.Members {
		if strings.EqualFold(c.Members[i].ID, id) {
			return &c.Members[i], true
		}
	}
	return nil, false
}

// NewToken makes a personal access token and its stored hash.
func NewToken() (token, hash string, err error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = "hpk_" + hex.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken is how tokens are stored.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ByToken finds the member a token belongs to, comparing in constant time.
func (c *Config) ByToken(token string) (*Person, bool) {
	if !strings.HasPrefix(token, "hpk_") {
		return nil, false
	}
	h := []byte(HashToken(token))
	var found *Person
	for i := range c.Members {
		if c.Members[i].TokenHash != "" && subtle.ConstantTimeCompare(h, []byte(c.Members[i].TokenHash)) == 1 {
			found = &c.Members[i]
		}
	}
	return found, found != nil
}

// Add puts a new member in the team and, in token mode, returns their token.
func (c *Config) Add(id, name, role string, projects []string, now time.Time) (string, error) {
	if _, ok := c.Find(id); ok {
		return "", fmt.Errorf("%s is already in the team", id)
	}
	if !ValidRole(role) {
		return "", fmt.Errorf("unknown role %q (owner, maintainer, member, viewer)", role)
	}
	m := Person{ID: id, Name: name, Role: role, Projects: projects, Added: now.UTC()}
	token := ""
	if c.Auth == "" || c.Auth == "token" {
		t, h, err := NewToken()
		if err != nil {
			return "", err
		}
		token, m.TokenHash = t, h
	}
	c.Members = append(c.Members, m)
	return token, c.check()
}

// Remove takes a member out. The last owner cannot be removed.
func (c *Config) Remove(id string) error {
	for i, m := range c.Members {
		if strings.EqualFold(m.ID, id) {
			if m.Role == Owner && c.owners() == 1 {
				return errors.New("the last owner cannot be removed; make someone else owner first")
			}
			c.Members = append(c.Members[:i], c.Members[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%s is not in the team", id)
}

// SetRole changes a member's role. The last owner keeps theirs.
func (c *Config) SetRole(id, role string) error {
	m, ok := c.Find(id)
	if !ok {
		return fmt.Errorf("%s is not in the team", id)
	}
	if !ValidRole(role) {
		return fmt.Errorf("unknown role %q", role)
	}
	if m.Role == Owner && role != Owner && c.owners() == 1 {
		return errors.New("the last owner must stay owner; make someone else owner first")
	}
	m.Role = role
	return nil
}

// Rotate gives a member a new token; the old one stops working.
func (c *Config) Rotate(id string) (string, error) {
	m, ok := c.Find(id)
	if !ok {
		return "", fmt.Errorf("%s is not in the team", id)
	}
	if c.Auth == "header" {
		return "", errors.New("header mode has no tokens; the proxy signs people in")
	}
	t, h, err := NewToken()
	if err != nil {
		return "", err
	}
	m.TokenHash = h
	return t, nil
}

func (c *Config) owners() int {
	n := 0
	for _, m := range c.Members {
		if m.Role == Owner {
			n++
		}
	}
	return n
}

// SetBudget sets a person's own money caps (USD; 0 = none).
func (c *Config) SetBudget(id string, daily, monthly float64) error {
	m, ok := c.Find(id)
	if !ok {
		return fmt.Errorf("%s is not in the team", id)
	}
	if daily < 0 || monthly < 0 || daily > 100000 || monthly > 1000000 {
		return errors.New("caps are amounts in USD; 0 means no cap")
	}
	m.BudgetDaily, m.BudgetMonthly = daily, monthly
	return nil
}

// WideProxies lists trusted proxies that are ranges, not one address. Every
// machine in a range can send the identity header, so header mode should trust
// the proxy's own address only.
func (c *Config) WideProxies() []string {
	var out []string
	for _, p := range c.TrustedProxies {
		if _, n, err := net.ParseCIDR(p); err == nil {
			if ones, bits := n.Mask.Size(); ones < bits {
				out = append(out, p)
			}
		}
	}
	return out
}
