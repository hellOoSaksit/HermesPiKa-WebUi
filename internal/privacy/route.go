// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package privacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hpk.local/hpk/internal/hermes"
	"hpk.local/hpk/internal/project"
)

// LocalOnly is the model policy that keeps a project's content on this machine:
// the agent may only use a model served from here (or a host the project lists).
const LocalOnly = "local_only"

// Route is where Hermes sends a model request.
type Route struct {
	Provider string `json:"provider,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	Model    string `json:"model,omitempty"`
}

// Providers that run on this machine when no base_url is set.
var localDefaults = map[string]bool{"lmstudio": true, "ollama": true, "llamacpp": true, "llama.cpp": true, "vllm": true}

// Host is the endpoint host, or "" when the route has no URL.
func (r Route) Host() string {
	if r.BaseURL == "" {
		return ""
	}
	u, err := url.Parse(r.BaseURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// Where names the route for people: the host, else the provider.
func (r Route) Where() string {
	if h := r.Host(); h != "" {
		return h
	}
	if r.Provider != "" {
		return r.Provider
	}
	return "unknown"
}

// IsLocal reports whether a request on this route stays on this machine.
// Only loopback counts, plus hosts the project lists in privacy.local_hosts
// (a GPU box on your own network, say). Provider names are not trusted on their
// own: an "lmstudio" route pointed at a remote URL is remote. A loopback proxy
// that forwards elsewhere cannot be seen from here; docs/privacy.md says so.
func IsLocal(r Route, extraHosts []string) bool {
	if r.BaseURL == "" {
		return localDefaults[strings.ToLower(r.Provider)]
	}
	h := r.Host()
	if h == "" {
		return false
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
		return true
	}
	for _, x := range extraHosts {
		if strings.EqualFold(strings.TrimSpace(x), h) {
			return true
		}
	}
	return false
}

// Seen is what hpk knows about the routes one Hermes session has used.
type Seen struct {
	Session string    `json:"session"`
	Last    Route     `json:"last"`
	Remote  []string  `json:"remote,omitempty"` // every non-loopback host this session sent a request to
	At      time.Time `json:"at"`
	// Usage as Hermes reports it after each request (main and auxiliary).
	Calls      int   `json:"calls,omitempty"`
	Input      int64 `json:"input_tokens,omitempty"`
	Output     int64 `json:"output_tokens,omitempty"`
	CacheRead  int64 `json:"cache_read_tokens,omitempty"`
	CacheWrite int64 `json:"cache_write_tokens,omitempty"`
	// Cost in USD at the models.dev prices Hermes caches; 0 when the model is unknown there.
	Cost float64 `json:"cost_usd,omitempty"`
}

// Weighted counts tokens at roughly what they cost: cache reads are billed at about
// a tenth of input on the providers Hermes supports, so they count a tenth here.
// Budgets use this, not the raw total, which counts every cached re-read in full.
func (s *Seen) Weighted() int64 { return s.Input + s.CacheWrite + s.Output + s.CacheRead/10 }

// Tokens is every token the session sent or received, cached or not.
func (s *Seen) Tokens() int64 { return s.Input + s.CacheWrite + s.CacheRead + s.Output }

func routeFile(home, session string) string {
	sum := sha256.Sum256([]byte(session))
	return filepath.Join(home, "hpk", "routes", hex.EncodeToString(sum[:8])+".json")
}

// RecordRoute notes one model request (main or auxiliary) of a session. Hosts that
// are not loopback are kept for the life of the record: once content went there,
// the session stays marked. It writes only when something changed.
func RecordRoute(home, session string, r Route) error {
	if session == "" {
		return nil
	}
	f := routeFile(home, session)
	s, _ := LoadSeen(home, session)
	if s == nil {
		s = &Seen{Session: session}
	}
	changed := s.Last != r
	s.Last = r
	if !IsLocal(r, nil) {
		w := r.Where()
		found := false
		for _, x := range s.Remote {
			found = found || x == w
		}
		if !found {
			s.Remote = append(s.Remote, w)
			changed = true
		}
	}
	if !changed && time.Since(s.At) < time.Hour {
		return nil
	}
	s.At = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	raw, _ := json.Marshal(s)
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	pruneRoutes(filepath.Dir(f))
	return os.Rename(tmp, f)
}

// RecordUsage adds one request's usage to the session record.
func RecordUsage(home, session string, r Route, usage map[string]any) error {
	if session == "" {
		return nil
	}
	s, _ := LoadSeen(home, session)
	if s == nil {
		s = &Seen{Session: session, Last: r}
	}
	num := func(keys ...string) int64 {
		for _, k := range keys {
			if v, ok := usage[k].(float64); ok {
				return int64(v)
			}
		}
		return 0
	}
	in, out := num("input_tokens", "prompt_tokens"), num("output_tokens", "completion_tokens")
	cr, cw := num("cache_read_tokens", "cache_read_input_tokens"), num("cache_write_tokens", "cache_creation_input_tokens")
	s.Calls++
	s.Input += in
	s.Output += out
	s.CacheRead += cr
	s.CacheWrite += cw
	if pr, ok := PriceOf(home, r.Provider, r.Model); ok {
		s.Cost += pr.cost(in, out, cr, cw)
	}
	return saveSeen(home, s)
}

func saveSeen(home string, s *Seen) error {
	f := routeFile(home, s.Session)
	s.At = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	raw, _ := json.Marshal(s)
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}

// LoadSeen returns the record of a session, or nil when hpk has seen no request.
func LoadSeen(home, session string) (*Seen, error) {
	raw, err := os.ReadFile(routeFile(home, session))
	if err != nil {
		return nil, err
	}
	var s Seen
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s.Session != session {
		return nil, fmt.Errorf("route record mismatch")
	}
	return &s, nil
}

// Records older than this are dropped; sessions that old are long over.
const routeKeep = 14 * 24 * time.Hour

func pruneRoutes(dir string) {
	entries, _ := os.ReadDir(dir)
	if len(entries) < 200 {
		return
	}
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > routeKeep {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// CheckModel decides whether a session may use tools on a local_only project.
// It fails closed: a session hpk has no route for (the model_route hook is not
// installed, or the record is unreadable) is refused.
func CheckModel(home string, p *project.Project, session string) (Verdict, string) {
	if p == nil || p.Privacy.ModelPolicy != LocalOnly {
		return Allow, ""
	}
	s, err := LoadSeen(home, session)
	if err != nil || s == nil {
		return Block, fmt.Sprintf("%s allows local models only, and hpk has not seen which model this session uses (is the model_route hook installed? run hpk doctor) (fail closed)", p.Name)
	}
	var out []string
	for _, h := range s.Remote {
		if !IsLocal(Route{BaseURL: "http://" + h}, p.Privacy.LocalHosts) && !IsLocal(Route{Provider: h}, p.Privacy.LocalHosts) {
			out = append(out, h)
		}
	}
	if len(out) > 0 {
		return Block, fmt.Sprintf("%s allows local models only, and this session has sent requests to %s. Start a new session on a local model (hpk refuses tools for this one)", p.Name, strings.Join(out, ", "))
	}
	if !IsLocal(s.Last, p.Privacy.LocalHosts) {
		return Block, fmt.Sprintf("%s allows local models only; this session uses %s", p.Name, s.Last.Where())
	}
	return Allow, ""
}

// CheckLaunch is the check before hpk itself starts model work (a Console session,
// a cron job) on a local_only project: the profile's configured route must be local.
func CheckLaunch(p *project.Project, r Route, profile string) error {
	if p == nil || p.Privacy.ModelPolicy != LocalOnly {
		return nil
	}
	if IsLocal(r, p.Privacy.LocalHosts) {
		return nil
	}
	return fmt.Errorf("%s allows local models only, but Hermes profile %q sends requests to %s. Point that profile at a local model (model.base_url on localhost) or add the host to privacy.local_hosts", p.Name, profile, r.Where())
}

// CheckActiveProfile runs CheckLaunch against the profile hermes and hermes-acp
// start with. It returns the profile name and its route for display.
func CheckActiveProfile(home string, p *project.Project) (string, Route, error) {
	prof := hermes.ActiveProfile(home)
	r := hermes.RouteOf(home, prof)
	route := Route{Provider: r.Provider, BaseURL: r.BaseURL, Model: r.Model}
	return prof, route, CheckLaunch(p, route, prof)
}
