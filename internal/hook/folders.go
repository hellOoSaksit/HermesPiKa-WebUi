// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package hook

import (
	"crypto/sha256"
	"encoding/hex"
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
	rules, st := folders.Load(home, p)
	text := folderText(p, rules, st)
	sum := sha256.Sum256([]byte(text))
	key := hex.EncodeToString(sum[:8])
	idSum := sha256.Sum256([]byte(ev.SessionID))
	file := filepath.Join(home, "hpk", "sessions", hex.EncodeToString(idSum[:8])+".folders")
	if old, err := os.ReadFile(file); err == nil && strings.TrimSpace(string(old)) == key {
		return result{Allow, ""}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err == nil {
		_ = os.WriteFile(file, []byte(key+"\n"), 0o644)
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
	rules, st := folders.Load(home, p)
	if st != folders.OK {
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
