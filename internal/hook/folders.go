// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package hook

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hpk.local/hpk/internal/folders"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// folderContext tells the AI about the project's folder rules at the start of a
// turn: once per session, and again only when the rules or their state change.
// What it last said is kept in <home>/hpk/sessions/<id>.folders.
func folderContext(ev Event) result {
	if ev.SessionID == "" {
		return result{Allow, ""}
	}
	home := project.Home()
	p, err := privacy.ResolveSession(home, ev.Cwd, ev.SessionID)
	if err != nil || p == nil {
		return result{Allow, ""}
	}
	idSum := sha256.Sum256([]byte(ev.SessionID))
	file := filepath.Join(home, "hpk", "sessions", hex.EncodeToString(idSum[:8])+".folders")
	fp := fingerprint(home, p)
	oldKey, oldFp := "", ""
	if old, err := os.ReadFile(file); err == nil {
		lines := strings.SplitN(strings.TrimSpace(string(old)), "\n", 2)
		oldKey = lines[0]
		if len(lines) > 1 {
			oldFp = lines[1]
		}
	}
	if oldKey != "" && oldFp == fp {
		return result{Allow, ""} // nothing the rules depend on changed: no read, no parse
	}
	rules, st := folders.Load(home, p)
	text := folderText(p, rules, st)
	sum := sha256.Sum256([]byte(text))
	key := hex.EncodeToString(sum[:8])
	if key != oldKey || fp != oldFp {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err == nil {
			_ = os.WriteFile(file, []byte(key+"\n"+fp+"\n"), 0o644)
		}
	}
	if key == oldKey {
		return result{Allow, ""}
	}
	return result{Inform, text}
}

func folderText(p *project.Project, r folders.Rules, st folders.State) string {
	switch st {
	case folders.Pending:
		n := ""
		if ds, err := wiki.Open(p.WikiDir()).Drafts(); err == nil {
			for _, d := range ds {
				if d.Meta.Target == folders.Page {
					n = fmt.Sprintf(" (draft #%d)", d.N)
					break
				}
			}
		}
		return "[hpk] Folder rules are waiting for approval" + n + ". Do not ask again."
	case folders.OK:
		if len(r.Rows) == 0 {
			return "[hpk] This project has no folder split: save files where they belong, no rule folders."
		}
		var parts []string
		for _, row := range r.Rows {
			parts = append(parts, fmt.Sprintf("`%s` (%s)", row.Folder, strings.Join(row.Exts, " ")))
		}
		return "[hpk] Project folders: " + strings.Join(parts, ", ") + ". Save each file in its folder."
	case folders.Changed:
		return "[hpk] The folder rules page was changed outside approval; ask the person to approve it again. Save files in the project root until then."
	}
	return "[hpk] Before saving the first file into this project, ask the person which folders to use and what goes in each, then draft `decisions/folders.md` with the table `| Folder | What goes here | Extensions |` (hpk wiki draft). If they want no split, draft the page with the header only."
}

// misplacedNote (post_tool_call) gently says where a file just saved belongs.
// It never blocks.
func misplacedNote(ev Event) result {
	if ev.ToolInput == nil {
		return result{Allow, ""}
	}
	path := firstStr(ev.ToolInput, "path", "file_path")
	if path == "" {
		return result{Allow, ""}
	}
	home := project.Home()
	p, err := privacy.ResolveSession(home, ev.Cwd, ev.SessionID)
	if err != nil || p == nil {
		return result{Allow, ""}
	}
	rel := projectRel(p, ev.Cwd, path)
	if rel == "" || strings.HasPrefix(rel, ".") {
		return result{Allow, ""}
	}
	if _, err := os.Stat(filepath.Join(p.WikiDir(), filepath.FromSlash(folders.Page))); err != nil {
		return result{Allow, ""} // no page: no rules to apply
	}
	rules, ok := cachedRules(home, p)
	if !ok {
		return result{Allow, ""}
	}
	want, ok := rules.For(rel)
	if !ok {
		return result{Allow, ""}
	}
	if len(rel) > len(want) && strings.EqualFold(rel[:len(want)+1], want+"/") {
		return result{Allow, ""}
	}
	return result{Inform, fmt.Sprintf("[hpk] %s belongs in %s/ by the project's folder rules", rel, want)}
}

// fingerprint is a cheap stand-in for "did anything the rules depend on change":
// size and mtime of the page and of the trusted-hash file, mtime of _drafts/.
// Only stat calls; nothing is read or parsed.
func fingerprint(home string, p *project.Project) string {
	st := func(path string, withSize bool) string {
		fi, err := os.Stat(path)
		if err != nil {
			return "-"
		}
		if withSize {
			return fmt.Sprintf("%d/%d", fi.ModTime().UnixNano(), fi.Size())
		}
		return fmt.Sprint(fi.ModTime().UnixNano())
	}
	return strings.Join([]string{
		st(filepath.Join(p.WikiDir(), filepath.FromSlash(folders.Page)), true),
		st(folders.TrustFile(home, p.Name), true),
		st(filepath.Join(p.WikiDir(), "_drafts"), false),
	}, " ")
}

type rulesCache struct {
	Fp    string
	OK    bool
	Rules []folders.Rule
}

// cachedRules returns the approved rules (ok=false when they cannot be used).
// While the stat-only fingerprint is unchanged the answer comes from a small
// per-project cache file; folders.Load runs only when the fingerprint moved.
func cachedRules(home string, p *project.Project) (folders.Rules, bool) {
	fp := fingerprint(home, p)
	file := filepath.Join(home, "hpk", "sessions", "folders-rules-"+hex.EncodeToString([]byte(p.Name))+".json")
	if raw, err := os.ReadFile(file); err == nil {
		var c rulesCache
		if json.Unmarshal(raw, &c) == nil && c.Fp == fp {
			return folders.Rules{Rows: c.Rules}, c.OK
		}
	}
	rules, st := folders.Load(home, p)
	c := rulesCache{Fp: fp, OK: st == folders.OK, Rules: rules.Rows}
	if raw, err := json.Marshal(c); err == nil {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err == nil {
			_ = os.WriteFile(file, raw, 0o644)
		}
	}
	return rules, c.OK
}
