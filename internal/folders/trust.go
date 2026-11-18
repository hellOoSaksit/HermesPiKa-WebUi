// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package folders

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// State says whether the rules page can be used.
type State string

const (
	None    State = "none"    // no page and no draft
	Pending State = "pending" // a draft waits for approval
	OK      State = "ok"      // the page is the one the person approved
	Changed State = "changed" // the page differs from the approved one, or was never approved
)

// TrustedDir holds the approved hashes. The AI's account gets it read-only.
func TrustedDir(home string) string { return filepath.Join(home, "hpk", "trusted") }

// TrustFile is where the approved hash of project name's rules page is kept.
func TrustFile(home, name string) string {
	return filepath.Join(TrustedDir(home), "folders-"+name+".sha256")
}

func pagePath(p *project.Project) string {
	return filepath.Join(p.WikiDir(), filepath.FromSlash(Page))
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func bodyOf(raw []byte) string {
	if pg, err := wiki.Parse(raw); err == nil {
		return pg.Body
	}
	return string(raw)
}

// Load returns the rules and their state. Rules are usable only when the state is OK.
func Load(home string, p *project.Project) (Rules, State) {
	raw, err := os.ReadFile(pagePath(p))
	if err != nil {
		if ds, e := wiki.Open(p.WikiDir()).Drafts(); e == nil {
			for _, d := range ds {
				if d.Meta.Target == Page {
					return Rules{}, Pending
				}
			}
		}
		return Rules{}, None
	}
	r := Parse(bodyOf(raw))
	want, err := os.ReadFile(TrustFile(home, p.Name))
	if err != nil || strings.TrimSpace(string(want)) != sum(raw) {
		return r, Changed
	}
	return r, OK
}

// Trust saves the hash of the page as it is on disk now.
func Trust(home string, p *project.Project) error {
	raw, err := os.ReadFile(pagePath(p))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(TrustedDir(home), 0o755); err != nil {
		return err
	}
	return os.WriteFile(TrustFile(home, p.Name), []byte(sum(raw)+"\n"), 0o644)
}

// Ensure creates the missing folders of the rules under root.
func Ensure(root string, r Rules) (created []string, err error) {
	for _, row := range r.Rows {
		if why := SafeFolder(row.Folder); why != "" {
			return created, fmt.Errorf("folder %q: %s", row.Folder, why)
		}
		dir := filepath.Join(root, filepath.FromSlash(row.Folder))
		if st, e := os.Stat(dir); e == nil {
			if !st.IsDir() {
				return created, fmt.Errorf("folder %q: a file with this name exists", row.Folder)
			}
			continue
		} else if !errors.Is(e, os.ErrNotExist) {
			return created, e
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return created, err
		}
		created = append(created, row.Folder)
	}
	return created, nil
}

// AfterApprove runs after wiki approval. When the rules page was approved it
// saves the trusted hash, creates the missing folders and writes the audit line.
// Callers log the error and carry on: it never fails the approval itself.
func AfterApprove(home string, p *project.Project, res []wiki.Result) error {
	hit := false
	for _, r := range res {
		if r.Target == Page && r.Done == "approved" {
			hit = true
		}
	}
	if !hit {
		return nil
	}
	if err := Trust(home, p); err != nil {
		return err
	}
	raw, err := os.ReadFile(pagePath(p))
	if err != nil {
		return err
	}
	rules := Parse(bodyOf(raw))
	created, err := Ensure(p.Root, rules)
	detail := fmt.Sprintf("%s rules=%d created=%d", p.Name, len(rules.Rows), len(created))
	if aerr := project.Audit(p.Dir, "folders.approved", detail); err == nil {
		err = aerr
	}
	return err
}
