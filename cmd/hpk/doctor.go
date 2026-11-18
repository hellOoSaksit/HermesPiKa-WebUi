// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/doctor"
	"hpk.local/hpk/internal/hermes"
	"hpk.local/hpk/internal/hook"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// runDoctor prints the doctor report (internal/doctor). Read-only.
func runDoctor(args []string, stdout io.Writer) int {
	cs := doctor.Run(context.Background(), project.Home(), "http://127.0.0.1:7878")
	if hasFlag(args, "--json") {
		printJSON(stdout, cs)
	} else {
		mark := map[string]string{"ok": "✓", "warn": "!", "fail": "✗", "info": "·"}
		for _, x := range cs {
			fmt.Fprintln(stdout, fmt.Sprintf("%s %-18s %s", mark[x.Level], x.Name, x.Detail))
			if x.Fix != "" && x.Level != "ok" {
				fmt.Fprintln(stdout, fmt.Sprintf("  %-18s → %s", "", x.Fix))
			}
		}
	}
	if doctor.Failed(cs) {
		return 1
	}
	return 0
}

// runSelftest exercises hpk's core flows in a throwaway folder (§15: run after
// every Hermes update). It never touches the real Hermes home or a model.
func runSelftest(stdout io.Writer) int {
	tmp, err := os.MkdirTemp("", "hpk-selftest-")
	if err != nil {
		fmt.Fprintln(stdout, "✗ temp dir:", err)
		return 1
	}
	defer os.RemoveAll(tmp)
	old := os.Getenv("HPK_HOME")
	os.Setenv("HPK_HOME", tmp)
	defer os.Setenv("HPK_HOME", old)
	root := filepath.Join(tmp, "code", "selftest-app")
	os.MkdirAll(root, 0o755)
	exec.Command("git", "-C", root, "init", "-q").Run()

	fails := 0
	step := func(name string, fn func() error) {
		t0 := time.Now()
		err := fn()
		ms := time.Since(t0).Milliseconds()
		if err != nil {
			fails++
			fmt.Fprintf(stdout, "✗ %-34s %v\n", name, err)
			return
		}
		fmt.Fprintf(stdout, "✓ %-34s %d ms\n", name, ms)
	}
	var p *project.Project
	step("project + wiki", func() error {
		var err error
		if p, err = project.Create(tmp, "selftest-app", root, project.Private); err != nil {
			return err
		}
		_, err = wiki.Open(p.WikiDir()).Init(p.Name, assets.WikiTemplates())
		return err
	})
	step("wiki draft needs a source", func() error {
		w := wiki.Open(p.WikiDir())
		if _, err := w.CreateDraft(wiki.DraftInput{Target: "lessons/x.md", Title: "X", Body: "x"}); err == nil {
			return fmt.Errorf("draft without source was accepted")
		}
		d, err := w.CreateDraft(wiki.DraftInput{Target: "lessons/x.md", Title: "X", Sources: []string{"selftest"}, Body: "A lesson."})
		if err != nil {
			return err
		}
		res, err := w.Approve([]int{d.N}, "selftest")
		if err != nil || res[0].Done != "approved" {
			return fmt.Errorf("approve: %v %v", res, err)
		}
		return nil
	})
	hookCall := func(name string, ev map[string]any) (string, int) {
		raw, _ := json.Marshal(ev)
		var out bytes.Buffer
		code := hook.Run(name, bytes.NewReader(raw), &out)
		return strings.TrimSpace(out.String()), code
	}
	step("red line asks a person", func() error {
		out, code := hookCall("red_line_guard", map[string]any{"hook_event_name": "pre_tool_call", "tool_name": "terminal",
			"tool_input": map[string]any{"command": "git push origin main"}, "cwd": root})
		if code != 0 || !strings.Contains(out, `"approve"`) {
			return fmt.Errorf("got %s (exit %d)", out, code)
		}
		return nil
	})
	step("broken guard fails closed", func() error {
		var out bytes.Buffer
		if code := hook.Run("red_line_guard", strings.NewReader("not json"), &out); code != 2 {
			return fmt.Errorf("exit %d, want 2", code)
		}
		return nil
	})
	step("private project keeps data in", func() error {
		out, code := hookCall("privacy_guard", map[string]any{"hook_event_name": "pre_tool_call", "tool_name": "send_message",
			"tool_input": map[string]any{"message": "status"}, "cwd": root})
		if code != 2 || !strings.Contains(out, "block") {
			return fmt.Errorf("got %s (exit %d)", out, code)
		}
		if v, _ := privacy.Decide(p, privacy.Call{Tool: "read_file", Input: map[string]any{"path": "README.md"}}); v != privacy.Allow {
			return fmt.Errorf("ordinary read was blocked")
		}
		return nil
	})
	step("board stops duplicate work", func() error {
		db, err := board.Open(p)
		if err != nil {
			return err
		}
		defer db.Close()
		if _, _, err := db.ClaimWork(board.Scope{Task: "export", Paths: []string{"api/**"}}, "a", "s1", false); err != nil {
			return err
		}
		if _, _, err := db.ClaimWork(board.Scope{Task: "Export"}, "b", "s2", true); err == nil {
			return fmt.Errorf("duplicate was claimed")
		}
		return nil
	})
	step("hermes reachable", func() error {
		info := hermes.Detect(context.Background())
		if info.Error != "" {
			return fmt.Errorf("%s", info.Error)
		}
		return nil
	})
	if fails > 0 {
		fmt.Fprintf(stdout, "%d checks failed\n", fails)
		return 1
	}
	fmt.Fprintln(stdout, "all checks passed")
	return 0
}
