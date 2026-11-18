// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// config.yaml is edited as text, not re-encoded: YAML is parsed only to find
// line positions, and hpk inserts or removes whole lines there. Every other
// byte (comments, blank lines, quoting, CRLF, BOM) stays as the person wrote it.
// When the layout is unusual hpk stops and says what to add by hand.

// errManual means hpk will not guess; the message says what to do by hand.
var errManual = errors.New("unusual layout")

type text struct {
	lines   []string
	eol     string
	bom     bool
	finalNL bool
}

func load(raw string) *text {
	t := &text{eol: "\n"}
	if strings.HasPrefix(raw, "\ufeff") {
		t.bom, raw = true, strings.TrimPrefix(raw, "\ufeff")
	}
	if strings.Contains(raw, "\r\n") {
		t.eol = "\r\n"
	}
	if raw == "" {
		t.finalNL = true
		return t
	}
	t.finalNL = strings.HasSuffix(raw, "\n")
	raw = strings.TrimSuffix(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	t.lines = strings.Split(raw, "\n")
	return t
}

func (t *text) String() string {
	if len(t.lines) == 0 {
		return ""
	}
	s := strings.Join(t.lines, t.eol)
	if t.finalNL {
		s += t.eol
	}
	if t.bom {
		s = "\ufeff" + s
	}
	return s
}

func (t *text) splice(at, del int, add []string) {
	out := append([]string{}, t.lines[:at]...)
	out = append(out, add...)
	t.lines = append(out, t.lines[at+del:]...)
}

// root parses the current text and returns the top mapping (empty for an empty file).
func (t *text) root() (*yaml.Node, error) {
	src := strings.Join(t.lines, "\n")
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.MappingNode}, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("config.yaml does not hold a mapping at the top")
	}
	return doc.Content[0], nil
}

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
func pad(n int) string      { return strings.Repeat(" ", n) }

// spanEnd is the line after the block that starts at line start with the given
// indent. Trailing blank lines and comments that sit at or left of the indent
// belong to what follows, not to this block.
func (t *text) spanEnd(start, indent int, allowDash bool) int {
	end := len(t.lines)
	for j := start + 1; j < len(t.lines); j++ {
		tr := strings.TrimSpace(t.lines[j])
		if tr == "" || strings.HasPrefix(tr, "#") {
			continue
		}
		ind := indentOf(t.lines[j])
		dash := tr == "-" || strings.HasPrefix(tr, "- ")
		if ind < indent || (ind == indent && !(allowDash && dash)) {
			end = j
			break
		}
	}
	for end > start+1 {
		s := t.lines[end-1]
		tr := strings.TrimSpace(s)
		if tr == "" || (strings.HasPrefix(tr, "#") && indentOf(s) <= indent) {
			end--
			continue
		}
		break
	}
	return end
}

// dashIndent is the column of the "- " that starts a block sequence item.
func (t *text) dashIndent(item *yaml.Node) (int, bool) {
	if item.Line < 1 || item.Line > len(t.lines) {
		return 0, false
	}
	s := t.lines[item.Line-1]
	tr := strings.TrimSpace(s)
	if !strings.HasPrefix(tr, "- ") {
		return 0, false
	}
	return indentOf(s), true
}

func pair(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

func isNull(v *yaml.Node) bool { return v.Kind == yaml.ScalarNode && v.Tag == "!!null" }
func isFlow(v *yaml.Node) bool { return v.Style&yaml.FlowStyle != 0 }

// bareKey reports whether the key's line is just "key:" (plus an optional comment),
// and turns "key: {}" / "key: []" into "key:" so children can follow.
func (t *text) bareKey(k *yaml.Node) bool {
	l := k.Line - 1
	re := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(k.Value) + `:)\s*(\{\s*\}|\[\s*\])?\s*(#.*)?$`)
	m := re.FindStringSubmatch(t.lines[l])
	if m == nil {
		return false
	}
	t.lines[l] = m[1]
	if m[3] != "" {
		t.lines[l] += " " + m[3]
	}
	return true
}

func emptyFlow(v *yaml.Node) bool {
	return (v.Kind == yaml.MappingNode || v.Kind == yaml.SequenceNode) && isFlow(v) && len(v.Content) == 0
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimRight(a[i], " ") != b[i] {
			return false
		}
	}
	return true
}

// ensureItem makes root[k1][k2] a block sequence that holds one item matching
// match, rendered by render(dashIndent). An existing matching item is replaced
// in place when it differs, so its position never moves.
func (t *text) ensureItem(k1, k2 string, match func(*yaml.Node) bool, render func(int) []string) (changed, replaced bool, err error) {
	root, err := t.root()
	if err != nil {
		return false, false, err
	}
	kn1, v1 := pair(root, k1)
	if kn1 == nil {
		add := append([]string{k1 + ":", "  " + k2 + ":"}, render(4)...)
		if n := len(t.lines); n > 0 && strings.TrimSpace(t.lines[n-1]) != "" {
			add = append([]string{""}, add...)
		}
		t.splice(len(t.lines), 0, add)
		return true, false, nil
	}
	i1 := kn1.Column - 1
	if isNull(v1) || emptyFlow(v1) {
		if !t.bareKey(kn1) {
			return false, false, errManual
		}
		t.splice(kn1.Line, 0, append([]string{pad(i1+2) + k2 + ":"}, render(i1+4)...))
		return true, false, nil
	}
	if v1.Kind != yaml.MappingNode || isFlow(v1) {
		return false, false, errManual
	}
	ci := v1.Content[0].Column - 1
	kn2, v2 := pair(v1, k2)
	if kn2 == nil {
		end := t.spanEnd(kn1.Line-1, i1, false)
		t.splice(end, 0, append([]string{pad(ci) + k2 + ":"}, render(ci+2)...))
		return true, false, nil
	}
	if isNull(v2) || emptyFlow(v2) {
		if !t.bareKey(kn2) {
			return false, false, errManual
		}
		t.splice(kn2.Line, 0, render(ci+2))
		return true, false, nil
	}
	if v2.Kind != yaml.SequenceNode || isFlow(v2) {
		return false, false, errManual
	}
	for _, it := range v2.Content {
		if !match(it) {
			continue
		}
		d, ok := t.dashIndent(it)
		if !ok {
			return false, false, errManual
		}
		start := it.Line - 1
		end := t.spanEnd(start, d, false)
		want := render(d)
		if equalLines(t.lines[start:end], want) {
			return false, false, nil
		}
		t.splice(start, end-start, want)
		return true, true, nil
	}
	d, ok := t.dashIndent(v2.Content[0])
	if !ok {
		return false, false, errManual
	}
	end := t.spanEnd(kn2.Line-1, kn2.Column-1, true)
	t.splice(end, 0, render(d))
	return true, false, nil
}

// removeItems deletes every item of root[k1][*] that matches, then removes keys
// that hpk left empty. It returns a description per removed item.
func (t *text) removeItems(k1 string, match func(*yaml.Node) bool, describe func(k2 string, it *yaml.Node) string) ([]string, error) {
	var out []string
	touched := map[string]bool{}
	for {
		root, err := t.root()
		if err != nil {
			return out, err
		}
		_, v1 := pair(root, k1)
		if v1 == nil || v1.Kind != yaml.MappingNode {
			return out, nil
		}
		found := false
		for i := 0; i+1 < len(v1.Content) && !found; i += 2 {
			k2, list := v1.Content[i], v1.Content[i+1]
			if list.Kind != yaml.SequenceNode {
				continue
			}
			for _, it := range list.Content {
				if !match(it) {
					continue
				}
				if isFlow(list) {
					return out, errManual
				}
				d, ok := t.dashIndent(it)
				if !ok {
					return out, errManual
				}
				start := it.Line - 1
				out = append(out, describe(k2.Value, it))
				t.splice(start, t.spanEnd(start, d, false)-start, nil)
				touched[k2.Value] = true
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	// Remove keys whose list hpk emptied, then the parent if hpk emptied it too.
	for {
		root, err := t.root()
		if err != nil {
			return out, err
		}
		kn1, v1 := pair(root, k1)
		if kn1 == nil {
			return out, nil
		}
		if isNull(v1) && len(touched) > 0 {
			l := kn1.Line - 1
			t.splice(l, t.spanEnd(l, kn1.Column-1, true)-l, nil)
			// install puts one blank line before a block it appends at the end; take it back too
			if l == len(t.lines) && l > 0 && strings.TrimSpace(t.lines[l-1]) == "" {
				t.lines = t.lines[:l-1]
			}
			return out, nil
		}
		gone := false
		for i := 0; i+1 < len(v1.Content); i += 2 {
			k2, v2 := v1.Content[i], v1.Content[i+1]
			if touched[k2.Value] && isNull(v2) {
				l := k2.Line - 1
				t.splice(l, t.spanEnd(l, k2.Column-1, true)-l, nil)
				delete(touched, k2.Value)
				touched[""] = true // the parent may now be empty
				gone = true
				break
			}
		}
		if !gone {
			return out, nil
		}
	}
}

// yq renders a YAML scalar with the quoting it needs.
func yq(s string) string {
	b, _ := yaml.Marshal(s)
	return strings.TrimSuffix(string(b), "\n")
}

func renderHook(p Paths, h Hook) func(int) []string {
	return func(d int) []string {
		in := pad(d + 2)
		out := []string{pad(d) + "- command: " + yq(p.command(h.Name))}
		if h.Matcher != "" {
			out = append(out, in+"matcher: "+yq(h.Matcher))
		}
		out = append(out, fmt.Sprintf("%stimeout: %d", in, h.Timeout))
		if h.FailClosed {
			out = append(out, in+"fail_closed: true")
		}
		return out
	}
}

func entryCommand(e *yaml.Node) string {
	if e.Kind != yaml.MappingNode {
		return ""
	}
	if _, c := pair(e, "command"); c != nil {
		return c.Value
	}
	return ""
}

func isOurSkills(v string) bool {
	return strings.HasSuffix(strings.ToLower(filepath.ToSlash(v)), "/hpk/skills")
}

func manual(p Paths, what string) error {
	return fmt.Errorf("%s: %s in %s is laid out in a way hpk will not guess at; add it by hand (see docs/install.md) and run hpk doctor", errManual, what, p.Config)
}

// PlanInstall computes the new config.yaml without writing anything.
func PlanInstall(p Paths) (Plan, error) {
	return plan(p, func(t *text) ([]string, error) {
		var ch []string
		want := filepath.ToSlash(p.SkillsDir)
		changed, _, err := t.ensureItem("skills", "external_dirs",
			func(n *yaml.Node) bool { return filepath.ToSlash(n.Value) == want },
			func(d int) []string { return []string{pad(d) + "- " + yq(want)} })
		if errors.Is(err, errManual) {
			return nil, manual(p, "skills.external_dirs")
		}
		if err != nil {
			return nil, err
		}
		if changed {
			ch = append(ch, "skills.external_dirs: add "+want)
		}
		for _, h := range Hooks {
			h := h
			changed, replaced, err := t.ensureItem("hooks", h.Event,
				func(n *yaml.Node) bool {
					c := entryCommand(n)
					return isOurs(c) && strings.HasSuffix(strings.TrimSpace(c), " "+h.Name)
				}, renderHook(p, h))
			if errors.Is(err, errManual) {
				return nil, manual(p, "hooks."+h.Event)
			}
			if err != nil {
				return nil, err
			}
			switch {
			case replaced:
				ch = append(ch, fmt.Sprintf("hooks.%s: update hpk %s", h.Event, h.Name))
			case changed:
				ch = append(ch, fmt.Sprintf("hooks.%s: add hpk %s (fail_closed=%v)", h.Event, h.Name, h.FailClosed))
			}
		}
		return ch, nil
	})
}

// PlanUninstall removes hpk's entries only; projects and wikis are never touched.
func PlanUninstall(p Paths) (Plan, error) {
	return plan(p, func(t *text) ([]string, error) {
		a, err := t.removeItems("skills", func(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && isOurSkills(n.Value) },
			func(_ string, n *yaml.Node) string { return "skills.external_dirs: remove " + n.Value })
		if errors.Is(err, errManual) {
			return nil, manual(p, "skills.external_dirs")
		}
		if err != nil {
			return nil, err
		}
		b, err := t.removeItems("hooks", func(n *yaml.Node) bool { return isOurs(entryCommand(n)) },
			func(ev string, n *yaml.Node) string { return "hooks." + ev + ": remove " + entryCommand(n) })
		if errors.Is(err, errManual) {
			return nil, manual(p, "hooks")
		}
		return append(a, b...), err
	})
}

func plan(p Paths, edit func(*text) ([]string, error)) (Plan, error) {
	pl := Plan{Paths: p}
	raw, err := os.ReadFile(p.Config)
	switch {
	case errors.Is(err, os.ErrNotExist):
		raw = nil
	case err != nil:
		return pl, err
	default:
		pl.Exists = true
	}
	pl.Before = string(raw)
	t := load(pl.Before)
	if _, err := t.root(); err != nil {
		return pl, fmt.Errorf("%s is not valid YAML (%v); hpk will not touch it", p.Config, err)
	}
	ch, err := edit(t)
	if err != nil {
		return pl, err
	}
	pl.After = t.String()
	if _, err := load(pl.After).root(); err != nil {
		return pl, fmt.Errorf("hpk produced invalid YAML for %s (%v); nothing was written", p.Config, err)
	}
	pl.Changes = ch
	pl.Diff = lineDiff(pl.Before, pl.After)
	return pl, nil
}
