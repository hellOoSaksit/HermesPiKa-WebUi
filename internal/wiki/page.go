// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package wiki

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Meta is a page's front-matter (docs/DESIGN.md §7.2), plus draft-only fields.
type Meta struct {
	Title      string   `yaml:"title"`
	Type       string   `yaml:"type"`
	Sources    []string `yaml:"sources"`
	Updated    string   `yaml:"updated"`
	Confidence string   `yaml:"confidence"`
	Origin     string   `yaml:"origin,omitempty"` // shared wiki: anonymous source project (§6.2)

	// Draft only: stripped when the draft is approved.
	Target   string `yaml:"target,omitempty"`   // where the page goes, e.g. lessons/csv-encoding.md
	Kind     string `yaml:"kind,omitempty"`     // new | edit
	Summary  string `yaml:"summary,omitempty"`  // one line: what is added or changed
	Author   string `yaml:"author,omitempty"`   // who drafted it (agent, session id, person)
	Created  string `yaml:"created,omitempty"`  // when the draft was made (RFC 3339)
	Promoted bool   `yaml:"promoted,omitempty"` // came from a project: a person must approve (red line)
}

// Page is a parsed wiki file.
type Page struct {
	Meta Meta
	Body string
}

var fence = []byte("---")

// Parse splits front-matter from the body. A file without front-matter has an empty Meta.
func Parse(raw []byte) (Page, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(raw, append(fence, '\n')) {
		return Page{Body: string(raw)}, nil
	}
	rest := raw[len(fence)+1:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		if bytes.HasSuffix(rest, []byte("\n---")) {
			end = len(rest) - 4
		} else {
			return Page{}, errors.New("front-matter is not closed with ---")
		}
	}
	var m Meta
	if err := yaml.Unmarshal(rest[:end], &m); err != nil {
		return Page{}, fmt.Errorf("front-matter: %w", err)
	}
	body := ""
	if after := end + 5; after <= len(rest) {
		body = string(rest[after:])
	}
	return Page{Meta: m, Body: strings.TrimLeft(body, "\n")}, nil
}

// Format writes a page back with its front-matter.
func Format(p Page) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	_ = enc.Encode(p.Meta)
	_ = enc.Close()
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(p.Body))
	b.WriteString("\n")
	return b.Bytes()
}

// Tokens estimates model tokens without a tokenizer: about 4 characters per token
// for ASCII text and about 2 per token for Thai and other scripts. Good enough for
// the size limits in §7.2 and §13; never used for billing.
func Tokens(s string) int {
	ascii, other := 0, 0
	for _, r := range s {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+3)/4 + (other+1)/2
}

// Secrets never enter the wiki (§7.2, PRODUCT.md). Same family of patterns as the Console.
var secretRE = regexp.MustCompile(`(?i)(sk-[a-z0-9_-]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY|\bpassword\s*[:=]\s*\S|\bapi[_-]?key\s*[:=]\s*\S|\b[A-Z_]*(SECRET|TOKEN)\s*=\s*\S|ghp_[A-Za-z0-9]{20,}|xox[bp]-[A-Za-z0-9-]{10,}|\b(AKIA|ASIA)[0-9A-Z]{16}\b)`)

// LooksSecret reports whether text appears to carry a credential.
func LooksSecret(s string) bool { return secretRE.MatchString(s) }

// Rules belong in the repository, not the wiki (§5.2, §7.2). This only flags lines
// that read like orders, for a person to look at; it never blocks.
// (Go's \b is ASCII-only, so the Thai words are matched without it.)
var ruleRE = regexp.MustCompile(`(?i)^\s*(?:[-*]\s*)?(?:(?:always|never|must|do not|don't|you must)\b|ต้อง|ห้าม|อย่า)`)

// RuleLines returns the 1-based line numbers that read like rules.
func RuleLines(body string) []int {
	var out []int
	for i, line := range strings.Split(body, "\n") {
		if ruleRE.MatchString(line) {
			out = append(out, i+1)
		}
	}
	return out
}
