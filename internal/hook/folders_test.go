// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/folders"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

const folderTable = "| Folder | What goes here | Extensions |\n|---|---|---|\n| RAW 3D | source scenes | .blend .max |\n| Export | exports | .fbx .obj |\n"

func folderProject(t *testing.T) (*project.Project, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HPK_HOME", home)
	t.Setenv("HPK_PROJECT", "")
	root := filepath.Join(home, "code", "orbit-app")
	os.MkdirAll(root, 0o755)
	p, err := project.Create(home, "orbit-app", root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wiki.Open(p.WikiDir()).Init(p.Name, assets.WikiTemplates()); err != nil {
		t.Fatal(err)
	}
	return p, home
}

func ctxOf(t *testing.T, out string) string {
	t.Helper()
	var r Reply
	if out != "" {
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("bad reply %q", out)
		}
	}
	return r.Context
}

func llmEvent(p *project.Project) string {
	return `{"hook_event_name":"pre_llm_call","session_id":"s-1","cwd":"` + filepath.ToSlash(p.Root) + `","extra":{"user_message":"hi"}}`
}

func TestFolderContextOncePerState(t *testing.T) {
	p, home := folderProject(t)
	out, _ := call("session_link", llmEvent(p))
	if c := ctxOf(t, out); !strings.Contains(c, "ask the person") {
		t.Fatalf("none: %q", c)
	}
	if out, _ := call("session_link", llmEvent(p)); out != "" {
		t.Fatalf("second call must be silent: %q", out)
	}
	w := wiki.Open(p.WikiDir())
	d, err := w.CreateDraft(wiki.DraftInput{Target: folders.Page, Title: "Folders", Sources: []string{"chat"}, Summary: "where files go", Author: "me", Body: folderTable})
	if err != nil {
		t.Fatal(err)
	}
	out, _ = call("session_link", llmEvent(p))
	if c := ctxOf(t, out); !strings.Contains(c, "waiting for approval") || !strings.Contains(c, "#"+string(rune('0'+d.N))) {
		t.Fatalf("pending: %q", c)
	}
	res, err := w.Approve([]int{d.N}, "me")
	if err != nil {
		t.Fatal(err)
	}
	if err := folders.AfterApprove(home, p, res); err != nil {
		t.Fatal(err)
	}
	out, _ = call("session_link", llmEvent(p))
	if c := ctxOf(t, out); !strings.Contains(c, "RAW 3D") || !strings.Contains(c, ".fbx") {
		t.Fatalf("ok: %q", c)
	}
	if out, _ := call("session_link", llmEvent(p)); out != "" {
		t.Fatalf("unchanged ok must be silent: %q", out)
	}
}

func TestMisplacedWriteWarns(t *testing.T) {
	p, home := folderProject(t)
	w := wiki.Open(p.WikiDir())
	d, err := w.CreateDraft(wiki.DraftInput{Target: folders.Page, Title: "Folders", Sources: []string{"chat"}, Summary: "x", Author: "me", Body: folderTable})
	if err != nil {
		t.Fatal(err)
	}
	res, _ := w.Approve([]int{d.N}, "me")
	if err := folders.AfterApprove(home, p, res); err != nil {
		t.Fatal(err)
	}
	write := func(path string) string {
		return `{"hook_event_name":"post_tool_call","tool_name":"write_file","tool_input":{"path":"` + path + `"},"session_id":"s-1","cwd":"` + filepath.ToSlash(p.Root) + `"}`
	}
	out, code := call("board_heartbeat", write("scene.fbx"))
	if c := ctxOf(t, out); code != 0 || !strings.Contains(c, "Export/") {
		t.Fatalf("root write: %q", out)
	}
	if out, _ := call("board_heartbeat", write("Export/scene.fbx")); out != "" {
		t.Fatalf("right place: %q", out)
	}
	if out, _ := call("board_heartbeat", write("notes.txt")); out != "" {
		t.Fatalf("no rule for .txt: %q", out)
	}
	// reading a file in the wrong place is not saving it: no note
	read := strings.Replace(write("scene.fbx"), `"tool_name":"write_file"`, `"tool_name":"read_file"`, 1)
	if out, _ := call("board_heartbeat", read); out != "" {
		t.Fatalf("read_file noted: %q", out)
	}
}

// A cron run has no one to ask: it is told to keep new files out instead.
func TestFolderTextForCron(t *testing.T) {
	p, _ := folderProject(t)
	if got := folderText(p, folders.Rules{}, folders.None, true); !strings.Contains(got, "do not save new files into the project") || strings.Contains(got, "ask the person") {
		t.Fatalf("cron: %q", got)
	}
	if got := folderText(p, folders.Rules{}, folders.None, false); !strings.Contains(got, "ask the person") {
		t.Fatalf("chat: %q", got)
	}
	if got := folderText(p, folders.Rules{}, folders.Changed, true); strings.Contains(got, "ask the person") {
		t.Fatalf("cron, changed: %q", got)
	}
}

func TestFolderContextIsLazy(t *testing.T) {
	p, home := folderProject(t)
	w := wiki.Open(p.WikiDir())
	d, err := w.CreateDraft(wiki.DraftInput{Target: folders.Page, Title: "Folders", Sources: []string{"chat"}, Summary: "x", Author: "me", Body: folderTable})
	if err != nil {
		t.Fatal(err)
	}
	res, _ := w.Approve([]int{d.N}, "me")
	if err := folders.AfterApprove(home, p, res); err != nil {
		t.Fatal(err)
	}
	if out, _ := call("session_link", llmEvent(p)); !strings.Contains(ctxOf(t, out), "RAW 3D") {
		t.Fatalf("first call: %q", out)
	}
	page := filepath.Join(p.WikiDir(), "decisions", "folders.md")
	fi, _ := os.Stat(page)
	raw, _ := os.ReadFile(page)
	// same size and mtime, different content: only a real read would notice
	alt := []byte(strings.Replace(string(raw), "RAW 3D", "RAW 4D", 1))
	if err := os.WriteFile(page, alt, 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(page, fi.ModTime(), fi.ModTime())
	if out, _ := call("session_link", llmEvent(p)); out != "" {
		t.Fatalf("unchanged fingerprint must not re-read: %q", out)
	}
	// a real change (new size) is noticed
	if err := os.WriteFile(page, append(alt, "\nmore\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _ := call("session_link", llmEvent(p)); !strings.Contains(ctxOf(t, out), "changed outside approval") {
		t.Fatalf("touched page: %q", out)
	}
}

func TestFolderContextNoticesNewDraft(t *testing.T) {
	p, _ := folderProject(t)
	call("session_link", llmEvent(p))
	if _, err := wiki.Open(p.WikiDir()).CreateDraft(wiki.DraftInput{Target: folders.Page, Title: "Folders", Sources: []string{"chat"}, Summary: "x", Author: "me", Body: folderTable}); err != nil {
		t.Fatal(err)
	}
	if out, _ := call("session_link", llmEvent(p)); !strings.Contains(ctxOf(t, out), "waiting for approval") {
		t.Fatalf("new draft: %q", out)
	}
}

func TestMisplacedWriteIsLazy(t *testing.T) {
	p, home := folderProject(t)
	w := wiki.Open(p.WikiDir())
	d, err := w.CreateDraft(wiki.DraftInput{Target: folders.Page, Title: "Folders", Sources: []string{"chat"}, Summary: "x", Author: "me", Body: folderTable})
	if err != nil {
		t.Fatal(err)
	}
	res, _ := w.Approve([]int{d.N}, "me")
	if err := folders.AfterApprove(home, p, res); err != nil {
		t.Fatal(err)
	}
	write := `{"hook_event_name":"post_tool_call","tool_name":"write_file","tool_input":{"path":"scene.fbx"},"session_id":"s-1","cwd":"` + filepath.ToSlash(p.Root) + `"}`
	if out, _ := call("board_heartbeat", write); !strings.Contains(ctxOf(t, out), "Export/") {
		t.Fatalf("first: %q", out)
	}
	page := filepath.Join(p.WikiDir(), "decisions", "folders.md")
	fi, _ := os.Stat(page)
	raw, _ := os.ReadFile(page)
	// same size and mtime, different content: a real Load would now say "changed"
	alt := []byte(strings.Replace(string(raw), "Export", "Exporz", 1))
	os.WriteFile(page, alt, 0o644)
	os.Chtimes(page, fi.ModTime(), fi.ModTime())
	if out, _ := call("board_heartbeat", write); !strings.Contains(ctxOf(t, out), "Export/") {
		t.Fatalf("unchanged fingerprint must use the cache: %q", out)
	}
	// the trusted hash being rewritten moves the fingerprint: reload sees a mismatch
	tf := folders.TrustFile(home, p.Name)
	if err := os.WriteFile(tf, []byte("0000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _ := call("board_heartbeat", write); out != "" {
		t.Fatalf("after a trust change the rules are not OK: %q", out)
	}
}
