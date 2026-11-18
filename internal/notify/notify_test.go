// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package notify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hpk.local/hpk/internal/project"
)

func TestSendRulesAndBatching(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "code", "sample-app")
	os.MkdirAll(root, 0o755)
	p, err := project.Create(home, "sample-app", root, project.Shared)
	if err != nil {
		t.Fatal(err)
	}
	var sent []string
	Sender = func(_ context.Context, to, text string) error { sent = append(sent, to+" "+text); return nil }
	now := time.Now()
	if ok, _ := Send(p, Approval, "x", now); ok {
		t.Fatal("no target: Console only")
	}
	if err := CheckTarget(p, "telegram; rm -rf /"); err == nil {
		t.Fatal("shell characters must be refused")
	}
	project.SetField(home, p.Name, []string{"notify", "to"}, "telegram", "test")
	p, _ = project.Load(home, p.Name)
	if ok, err := Send(p, Approval, "waiting", now); !ok || err != nil || len(sent) != 1 {
		t.Fatalf("first notice goes out: %v %v %v", ok, err, sent)
	}
	if ok, _ := Send(p, Approval, "again", now.Add(time.Minute)); ok {
		t.Fatal("same kind within 5 minutes is batched")
	}
	if ok, _ := Send(p, Duplicate, "two on one task", now.Add(time.Minute)); !ok {
		t.Fatal("duplicate-work alerts are never batched")
	}
	if ok, _ := Send(p, Approval, "later", now.Add(6*time.Minute)); !ok {
		t.Fatal("after the window it goes out again")
	}
	project.SetField(home, p.Name, []string{"privacy", "mode"}, "private", "test")
	p, _ = project.Load(home, p.Name)
	if ok, err := Send(p, Report, "done", now.Add(time.Hour)); ok || err != ErrLocal {
		t.Fatalf("private projects never send out: %v %v", ok, err)
	}
	if err := CheckTarget(p, "telegram"); err != ErrLocal {
		t.Fatal("a private project cannot get a target")
	}
}
