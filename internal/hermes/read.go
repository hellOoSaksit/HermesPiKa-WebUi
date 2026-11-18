// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package hermes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Profile is one Hermes profile as the Console shows it. Only the model is read
// from its config.yaml; nothing else, and never .env.
type Profile struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Default bool   `json:"default"`
}

func modelOf(cfg string) string {
	raw, err := os.ReadFile(cfg)
	if err != nil {
		return ""
	}
	var c struct {
		Model any `yaml:"model"`
	}
	if yaml.Unmarshal(raw, &c) != nil {
		return ""
	}
	switch m := c.Model.(type) {
	case string:
		return m
	case map[string]any:
		if s, ok := m["default"].(string); ok {
			return s
		}
	}
	return ""
}

// Profiles lists the default profile and every <home>/profiles/<name>.
func Profiles(home string) []Profile {
	out := []Profile{{Name: "default", Model: modelOf(filepath.Join(home, "config.yaml")), Default: true}}
	entries, _ := os.ReadDir(filepath.Join(home, "profiles"))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, Profile{Name: e.Name(), Model: modelOf(filepath.Join(home, "profiles", e.Name(), "config.yaml"))})
		}
	}
	sort.SliceStable(out[1:], func(i, j int) bool { return out[1+i].Name < out[1+j].Name })
	return out
}

// Credential is one entry of Hermes' credential pool, as `hermes auth list`
// prints it. Hermes never prints keys there, so hpk never sees one.
type Credential struct {
	Provider string `json:"provider"`
	Index    int    `json:"index"`
	Label    string `json:"label"`
	Type     string `json:"type"` // oauth | api_key …
	ID       string `json:"id"`
	Priority int    `json:"priority"`
	Source   string `json:"source"`
	Status   string `json:"status,omitempty"` // exhausted / cooldown text, if any
	Current  bool   `json:"current"`          // the one Hermes would use next
}

var (
	credHead = regexp.MustCompile(`^(\S+) \((\d+) credentials?\):\s*$`)
	credRow  = regexp.MustCompile(`^\s+#(\d+)\s+(.*?)\s+(\S+)\s+id=(\S+)\s+priority=(-?\d+)\s*(.*)$`)
)

// ParseAuthList reads the output of `hermes auth list`.
func ParseAuthList(out string) []Credential {
	var list []Credential
	provider := ""
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if m := credHead.FindStringSubmatch(line); m != nil {
			provider = m[1]
			continue
		}
		m := credRow.FindStringSubmatch(line)
		if m == nil || provider == "" {
			if strings.TrimSpace(line) == "" {
				provider = ""
			}
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		pr, _ := strconv.Atoi(m[5])
		rest := strings.TrimSpace(m[6])
		c := Credential{Provider: provider, Index: idx, Label: strings.TrimSpace(m[2]), Type: m[3], ID: m[4], Priority: pr}
		if strings.HasSuffix(rest, "←") {
			c.Current = true
			rest = strings.TrimSpace(strings.TrimSuffix(rest, "←"))
		}
		if f := strings.Fields(rest); len(f) > 0 {
			c.Source = f[0]
			c.Status = strings.TrimSpace(strings.TrimPrefix(rest, f[0]))
		}
		list = append(list, c)
	}
	return list
}

var (
	credMu   sync.Mutex
	credAt   time.Time
	credList []Credential
	credErr  error
)

// Credentials runs `hermes auth list` (about 3 s), cached for a minute.
func Credentials(ctx context.Context) ([]Credential, error) {
	credMu.Lock()
	defer credMu.Unlock()
	// A good list is kept a minute; a failure only briefly (Hermes may be busy starting).
	if keep := time.Minute; credErr != nil && time.Since(credAt) < 5*time.Second || credErr == nil && time.Since(credAt) < keep {
		return credList, credErr
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "hermes", "auth", "list").Output()
	credAt, credErr = time.Now(), err
	credList = ParseAuthList(string(out))
	return credList, credErr
}

// ModelRoute is a profile's configured model endpoint: model.provider, model.base_url
// and model.default from its config.yaml. Keys and .env are never read.
type ModelRoute struct {
	Profile  string
	Provider string
	BaseURL  string
	Model    string
}

// ActiveProfile is the profile a plain `hermes` (or hermes-acp) starts with:
// $HERMES_HOME when it points at a profile, else <home>/active_profile, else default.
func ActiveProfile(home string) string {
	if h := os.Getenv("HERMES_HOME"); h != "" {
		if filepath.Base(filepath.Dir(filepath.Clean(h))) == "profiles" {
			return filepath.Base(filepath.Clean(h))
		}
	}
	raw, err := os.ReadFile(filepath.Join(home, "active_profile"))
	if name := strings.TrimSpace(string(raw)); err == nil && name != "" {
		return name
	}
	return "default"
}

// RouteOf reads one profile's model route.
func RouteOf(home, profile string) ModelRoute {
	cfg := filepath.Join(home, "config.yaml")
	if profile != "" && profile != "default" {
		cfg = filepath.Join(home, "profiles", profile, "config.yaml")
	}
	r := ModelRoute{Profile: orDefault(profile)}
	raw, err := os.ReadFile(cfg)
	if err != nil {
		return r
	}
	var c struct {
		Model any `yaml:"model"`
	}
	if yaml.Unmarshal(raw, &c) != nil {
		return r
	}
	switch m := c.Model.(type) {
	case string:
		r.Model = m
	case map[string]any:
		r.Model = str(m["default"])
		if r.Model == "" {
			r.Model = str(m["model"])
		}
		r.Provider = str(m["provider"])
		r.BaseURL = str(m["base_url"])
		if r.BaseURL == "" {
			r.BaseURL = str(m["api_base"])
		}
	}
	return r
}

func orDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
