// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package procs

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestDescendants(t *testing.T) {
	list := []Proc{{PID: 10, PPID: 1}, {PID: 11, PPID: 10}, {PID: 12, PPID: 11}, {PID: 13, PPID: 1}}
	got := Descendants(list, 10)
	if len(got) != 2 || got[0] != 12 || got[1] != 11 {
		t.Fatalf("deepest first: %v", got)
	}
}

func TestListAndKill(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-NoProfile", "-Command", "Start-Sleep -Seconds 60")
	} else {
		cmd = exec.Command("sleep", "60")
	}
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer cmd.Process.Kill()
	ctx := context.Background()
	list, err := List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var start string
	for _, p := range list {
		if p.PID == cmd.Process.Pid {
			start = p.Start
		}
	}
	if start == "" {
		t.Fatal("the child is listed")
	}
	if err := Kill(ctx, cmd.Process.Pid, "not-the-same-start"); err != ErrGone {
		t.Fatalf("a reused id is not stopped: %v", err)
	}
	if err := Kill(ctx, cmd.Process.Pid, start); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("still running")
	}
}

func TestHermesAncestry(t *testing.T) {
	list := []Proc{{PID: 1, PPID: 0, Cmd: "init"}, {PID: 10, PPID: 1, Cmd: "/opt/hermes/bin/python -m hermes_cli gateway"},
		{PID: 11, PPID: 10, Cmd: "npm run dev"}, {PID: 12, PPID: 11, Cmd: "node vite"}, {PID: 20, PPID: 1, Cmd: "explorer.exe"}}
	if !UnderHermes(list, 12) || !UnderHermes(list, 11) {
		t.Error("children of the gateway are Hermes' jobs")
	}
	if UnderHermes(list, 20) || UnderHermes(list, 10) {
		t.Error("other programs (and the gateway itself) are not")
	}
	if !IsHermes(`C:\Users\u\AppData\Local\hermes\bin\hermes.exe gateway`) || IsHermes("hermesia.exe") {
		t.Error("IsHermes")
	}
}
