// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package project reads and writes project.yaml and knows where hpk keeps things
// (docs/DESIGN.md §5.1): <home>/hpk/projects/<name>/{project.yaml, wiki/, audit.log}
// and the shared wiki at <home>/hpk-shared/.
package project

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Privacy modes (§6.1).
const (
	Shared   = "shared"   // may promote to and read from the shared wiki
	Private  = "private"  // reads shared, never writes it; nothing leaves this machine
	Isolated = "isolated" // neither reads nor writes shared; nothing leaves this machine
)

// Project is the part of project.yaml hpk needs today. Unknown keys are kept on save.
type Project struct {
	Name     string `yaml:"name"`
	Root     string `yaml:"root"`
	Language string `yaml:"language,omitempty"`
	Privacy  struct {
		Mode        string   `yaml:"mode"`
		ModelPolicy string   `yaml:"model_policy,omitempty"`
		LocalHosts  []string `yaml:"local_hosts,omitempty"` // hosts besides loopback that count as this machine for local_only
	} `yaml:"privacy"`

	Dir string `yaml:"-"` // <home>/hpk/projects/<name>
}

// WikiDir is where this project's wiki lives: where the person moved it to
// (hpk/trusted/locations.json), else inside the project's own hpk folder.
func (p *Project) WikiDir() string {
	if d := WikiLocations(HomeOf(p))[p.Name]; d != "" {
		return d
	}
	return filepath.Join(p.Dir, "wiki")
}

// Mode returns the privacy mode, treating a missing value as the default (shared).
func (p *Project) Mode() string {
	if p.Privacy.Mode == "" {
		return Shared
	}
	return p.Privacy.Mode
}

// Home is the Hermes home hpk works in: $HPK_HOME, then $HERMES_HOME, then the
// Hermes default for this OS (verified in docs/m0-findings.md).
func Home() string {
	if v := os.Getenv("HPK_HOME"); v != "" {
		return v
	}
	if v := os.Getenv("HERMES_HOME"); v != "" {
		// Hermes points HERMES_HOME at <home>/profiles/<name> for a named profile;
		// projects belong to the whole home, not one profile (§5.1).
		clean := filepath.Clean(v)
		if filepath.Base(filepath.Dir(clean)) == "profiles" {
			return filepath.Dir(filepath.Dir(clean))
		}
		return clean
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "hermes")
		}
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".hermes")
}

// ProjectsDir and SharedDir are fixed under the home.
func ProjectsDir(home string) string { return filepath.Join(home, "hpk", "projects") }
func SharedDir(home string) string   { return filepath.Join(home, "hpk-shared") }

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// Create writes a new project.yaml. M3 adds the full analysis flow; this is the minimum.
func Create(home, name, root, mode string) (*Project, error) {
	if !validName.MatchString(name) {
		return nil, fmt.Errorf("project name %q: use lowercase letters, digits, . _ -", name)
	}
	if mode == "" {
		mode = Shared
	}
	if err := checkMode(mode); err != nil {
		return nil, err
	}
	dir := filepath.Join(ProjectsDir(home), name)
	if _, err := os.Stat(filepath.Join(dir, "project.yaml")); err == nil {
		return nil, fmt.Errorf("project %q already exists", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := &Project{Name: name, Root: root, Dir: dir}
	p.Privacy.Mode = mode
	p.Privacy.ModelPolicy = "any"
	raw, err := marshal2(p)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), raw, 0o644); err != nil {
		return nil, err
	}
	return p, Audit(dir, "project created", "privacy="+mode)
}

// Load reads <home>/hpk/projects/<name>/project.yaml.
func Load(home, name string) (*Project, error) {
	// a name is never a path: it comes from URLs, env and the CLI
	if !validName.MatchString(name) {
		return nil, fmt.Errorf("no project %q (names are a-z, 0-9, . _ -)", name)
	}
	dir := filepath.Join(ProjectsDir(home), name)
	raw, err := os.ReadFile(filepath.Join(dir, "project.yaml"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no project %q (looked in %s)", name, dir)
		}
		return nil, err
	}
	var p Project
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("project %s: %w", name, err)
	}
	if p.Name == "" {
		p.Name = name
	}
	p.Dir = dir
	if err := checkMode(p.Mode()); err != nil {
		return nil, fmt.Errorf("project %s: %w", name, err)
	}
	return &p, nil
}

// List returns every project under the home, by name.
func List(home string) ([]*Project, error) {
	entries, err := os.ReadDir(ProjectsDir(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if p, err := Load(home, e.Name()); err == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// FindByDir returns the project whose root contains dir (the deepest root wins).
func FindByDir(home, dir string) (*Project, error) {
	list, err := List(home)
	if err != nil {
		return nil, err
	}
	d := norm(dir)
	var best *Project
	for _, p := range list {
		r := norm(expand(p.Root))
		if r == "" {
			continue
		}
		if d == r || strings.HasPrefix(d, r+"/") {
			if best == nil || len(r) > len(norm(expand(best.Root))) {
				best = p
			}
		}
	}
	return best, nil
}

// SetPrivacy changes the mode. Loosening it (towards shared) needs a person to
// confirm, and never promotes old knowledge by itself (§6.3).
func SetPrivacy(home, name, mode string, confirmed bool) (*Project, error) {
	if err := checkMode(mode); err != nil {
		return nil, err
	}
	p, err := Load(home, name)
	if err != nil {
		return nil, err
	}
	from := p.Mode()
	if from == mode {
		return p, nil
	}
	if rank(mode) < rank(from) && !confirmed {
		return nil, fmt.Errorf("moving %s from %s to %s opens it up; a person must confirm (--yes). Old knowledge is not promoted", name, from, mode)
	}
	path := filepath.Join(p.Dir, "project.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Edit only privacy.mode in the YAML tree so comments and other keys survive.
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if err := setKey(&doc, []string{"privacy", "mode"}, mode); err != nil {
		return nil, err
	}
	out, err := marshal2(&doc)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return nil, err
	}
	p.Privacy.Mode = mode
	return p, Audit(p.Dir, "privacy changed", from+" → "+mode)
}

// Audit appends one line to the project's audit.log.
func Audit(dir, what, detail string) error {
	f, err := os.OpenFile(filepath.Join(dir, "audit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339), what, detail)
	return err
}

func checkMode(m string) error {
	switch m {
	case Shared, Private, Isolated:
		return nil
	}
	return fmt.Errorf("privacy mode %q: use shared, private or isolated", m)
}

// rank: higher is more closed.
func rank(m string) int {
	switch m {
	case Isolated:
		return 2
	case Private:
		return 1
	}
	return 0
}

func setKey(doc *yaml.Node, path []string, value string) error {
	n := doc
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for i, k := range path {
		if n.Kind != yaml.MappingNode {
			return errors.New("project.yaml is not a mapping")
		}
		var next *yaml.Node
		for j := 0; j+1 < len(n.Content); j += 2 {
			if n.Content[j].Value == k {
				next = n.Content[j+1]
			}
		}
		if next == nil {
			kind := yaml.MappingNode
			if i == len(path)-1 {
				kind = yaml.ScalarNode
			}
			next = &yaml.Node{Kind: kind}
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, next)
		}
		n = next
	}
	n.Kind, n.Tag, n.Value = yaml.ScalarNode, "!!str", value
	return nil
}

func expand(p string) string {
	if strings.HasPrefix(p, "~") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

// norm makes paths comparable: forward slashes, no trailing slash, case-folded on Windows.
func norm(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.ToSlash(filepath.Clean(p))
	p = strings.TrimSuffix(p, "/")
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// SetField sets one key of project.yaml (path like ["gates", "red_lines_extra"]) to
// value, editing the YAML tree so comments and other keys survive. A nil value
// removes the key. what goes to audit.log.
func SetField(home, name string, path []string, value any, what string) error {
	p, err := Load(home, name)
	if err != nil {
		return err
	}
	file := filepath.Join(p.Dir, "project.yaml")
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	n := doc.Content[0]
	for i, k := range path {
		if n.Kind != yaml.MappingNode {
			return errors.New("project.yaml is not a mapping at " + strings.Join(path[:i], "."))
		}
		at := -1
		for j := 0; j+1 < len(n.Content); j += 2 {
			if n.Content[j].Value == k {
				at = j
			}
		}
		last := i == len(path)-1
		if last {
			if value == nil {
				if at >= 0 {
					n.Content = append(n.Content[:at], n.Content[at+2:]...)
				}
				break
			}
			var v yaml.Node
			if err := v.Encode(value); err != nil {
				return err
			}
			if at >= 0 {
				old := n.Content[at+1]
				v.LineComment, v.HeadComment, v.FootComment = old.LineComment, old.HeadComment, old.FootComment
				n.Content[at+1] = &v
			} else {
				n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &v)
			}
			break
		}
		if at < 0 {
			child := &yaml.Node{Kind: yaml.MappingNode}
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, child)
			n = child
			continue
		}
		n = n.Content[at+1]
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	enc.Close()
	out := buf.Bytes()
	var check Project
	if err := yaml.Unmarshal(out, &check); err != nil {
		return fmt.Errorf("project.yaml would become invalid (%v); nothing was written", err)
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		return err
	}
	return Audit(p.Dir, what, strings.Join(path, ".")+" = "+fmt.Sprint(value))
}

// Field reads one key of project.yaml into out (zero value when missing).
func Field(p *Project, path []string, out any) error {
	raw, err := os.ReadFile(filepath.Join(p.Dir, "project.yaml"))
	if err != nil {
		return err
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return err
	}
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	if cur == nil {
		return nil
	}
	b, err := marshal2(cur)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, out)
}

// marshal2 encodes YAML with a 2-space indent, the style people write by hand
// (yaml.Marshal uses 4).
func marshal2(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
