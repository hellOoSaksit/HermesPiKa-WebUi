// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package board

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hpk.local/hpk/internal/project"
)

// Fictional people and paths only.
func open(t *testing.T) *DB {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, "code", "sample-app")
	os.MkdirAll(root, 0o755)
	p, err := project.Create(home, "sample-app", root, project.Shared)
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGlobs(t *testing.T) {
	yes := [][2]string{{"api/invoices/**", "api/invoices/totals.ts"}, {"web/src/**", "web/src/auth/**"}, {"api/x.go", "api/x.go"}, {"**", "anything/at/all.go"}}
	no := [][2]string{{"web/src/auth/**", "api/session/**"}, {"api/x.go", "api/y.go"}, {"docs/a.md", "docs/ab.md"}}
	for _, c := range yes {
		if !globsMeet(c[0], c[1]) || !globsMeet(c[1], c[0]) {
			t.Errorf("%v should meet", c)
		}
	}
	for _, c := range no {
		if globsMeet(c[0], c[1]) {
			t.Errorf("%v should not meet", c)
		}
	}
}

func TestClaimLevels(t *testing.T) {
	db := open(t)
	a, _, err := db.ClaimWork(Scope{Task: "Billing: invoice export", Lane: "billing", Paths: []string{"api/invoices/**"}, Wiki: []string{"architecture/billing.md"}}, "mali", "s-a", false)
	if err != nil {
		t.Fatal(err)
	}
	// 🔴 same task, written differently
	if _, ov, err := db.ClaimWork(Scope{Task: "billing – invoice-export"}, "ken", "s-b", true); !errors.Is(err, ErrNeedsPerson) || ov[0].Level != Duplicate {
		t.Fatalf("duplicate must stop even with --yes: %v %+v", err, ov)
	}
	// 🟠 file overlap: stops, then goes ahead when a person confirms
	sc := Scope{Task: "billing: rounding hotfix", Paths: []string{"api/invoices/totals.ts"}}
	if _, ov, err := db.ClaimWork(sc, "ken", "s-b", false); !errors.Is(err, ErrNeedsPerson) || ov[0].Level != Files {
		t.Fatalf("file overlap must ask: %v %+v", err, ov)
	}
	b, ov, err := db.ClaimWork(sc, "ken", "s-b", true)
	if err != nil || b.ID == "" || ov[0].Claim.ID != a.ID {
		t.Fatalf("confirmed overlap claims: %+v %v", b, err)
	}
	// 🟡 related only: claims without asking
	if _, ov, err := db.ClaimWork(Scope{Task: "billing docs", Lane: "billing", Wiki: []string{"architecture/billing.md"}}, "ploy", "s-c", false); err != nil || ov[0].Level != Related {
		t.Fatalf("related must not stop: %v %+v", err, ov)
	}
	who, _ := db.Who("api/invoices/totals.ts")
	if len(who) != 2 {
		t.Fatalf("who: %+v", who)
	}
	notices, _ := db.Notices(false)
	if len(notices) == 0 {
		t.Fatal("conflicts must notify the claim owner")
	}
}

func TestHeartbeatWidensAndStale(t *testing.T) {
	db := open(t)
	clock := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	db.Now = func() time.Time { return clock }
	db.ClaimWork(Scope{Task: "auth tokens", Paths: []string{"web/src/auth/**"}}, "mali", "s-a", false)
	db.ClaimWork(Scope{Task: "api tests", Paths: []string{"api/tests/**"}}, "ken", "s-b", false)
	ov, err := db.Heartbeat("s-a", []string{"api/tests/auth.test.ts"})
	if err != nil || len(ov) != 1 || ov[0].Claim.Owner != "ken" {
		t.Fatalf("widening into someone's files must be reported: %+v %v", ov, err)
	}
	list, _ := db.Who("s-a")
	_ = list
	c, _ := db.forRef("s-a")
	if !strings.Contains(strings.Join(c[0].Paths, " "), "api/tests/auth.test.ts") {
		t.Fatalf("scope must widen: %v", c[0].Paths)
	}
	clock = clock.Add(45 * time.Minute)
	db.Heartbeat("s-b", nil)
	stale, _ := db.MarkStale(30 * time.Minute)
	if len(stale) != 1 || stale[0].Session != "s-a" {
		t.Fatalf("stale: %+v", stale)
	}
	open, _ := db.Claims(false)
	if len(open) != 2 {
		t.Fatal("stale claims are never released automatically")
	}
	db.Heartbeat("s-a", nil)
	c, _ = db.forRef("s-a")
	if c[0].Status != Active {
		t.Fatal("a heartbeat revives a stale claim")
	}
	db.Release("s-a")
	if open, _ := db.Claims(false); len(open) != 1 {
		t.Fatal("release")
	}
}

func TestAskAnswerTimeout(t *testing.T) {
	db := open(t)
	clock := time.Now().UTC()
	db.Now = func() time.Time { return clock }
	db.ClaimWork(Scope{Task: "search reindex", Paths: []string{"api/search/**"}}, "ploy", "s-x", false)
	a, _ := db.Ask("s-me", "s-x", "still on the reindex?", "ploy")
	if !strings.HasPrefix(QueuedText(a), "/queue ") {
		t.Fatal("questions go in with /queue so they never interrupt")
	}
	q2, _ := db.Ask("s-me", "s-x", "and the docs?", "ploy")
	db.Answer(a.ID, map[string]string{"status": "working", "progress": "70%", "files": "api/search/index.go"})
	clock = clock.Add(11 * time.Minute)
	expired, _ := db.ExpireAsks(func(string) string { return "ploy" })
	if len(expired) != 1 || expired[0].ID != q2.ID {
		t.Fatalf("timeouts: %+v", expired)
	}
	asks, _ := db.Asks()
	if asks[len(asks)-1].Status != "answered" || asks[len(asks)-1].Answer["progress"] != "70%" {
		t.Fatalf("answer stored: %+v", asks)
	}
}

func TestBatching(t *testing.T) {
	db := open(t)
	for i := 0; i < 3; i++ {
		db.notify(Files, "stale", "mali", "c-1", "", nil, "x")
	}
	for i := 0; i < 2; i++ {
		db.notify(Duplicate, "conflict", "mali", "c-1", "", nil, "y")
	}
	n, _ := db.Notices(false)
	if len(n) != 3 {
		t.Fatalf("caution batched to 1, duplicates always sent (2): got %d", len(n))
	}
}

func TestRender(t *testing.T) {
	db := open(t)
	db.ClaimWork(Scope{Task: "auth tokens", Lane: "auth", Paths: []string{"web/src/auth/**"}}, "mali", "s-a", false)
	root := t.TempDir()
	if _, err := db.Render(root, "BOARD.md", "hpk"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "BOARD.md"))
	if !strings.Contains(string(raw), "auth tokens") {
		t.Fatalf("render hpk:\n%s", raw)
	}
	human := "# Team board\n\nOur notes.\n\n<!-- hpk:claims -->\nold\n<!-- /hpk:claims -->\n\nMore notes.\n"
	os.WriteFile(filepath.Join(root, "B2.md"), []byte(human), 0o644)
	if _, err := db.Render(root, "B2.md", "markdown"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(root, "B2.md"))
	if !strings.Contains(string(raw), "Our notes.") || !strings.Contains(string(raw), "More notes.") || strings.Contains(string(raw), "\nold\n") {
		t.Fatalf("markdown kind must only replace the marked block:\n%s", raw)
	}
	os.WriteFile(filepath.Join(root, "B3.md"), []byte("no markers"), 0o644)
	if _, err := db.Render(root, "B3.md", "markdown"); err == nil {
		t.Fatal("without markers hpk must not touch the file")
	}
}

func TestDiffWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	db := open(t)
	root := t.TempDir()
	git := func(dir string, args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git(root, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("1"), 0o644)
	git(root, "add", ".")
	git(root, "commit", "-qm", "init")
	for _, wt := range []string{"one", "two"} {
		dir := filepath.Join(root, "worktrees", wt)
		git(root, "worktree", "add", "-q", "-b", wt, dir)
		os.WriteFile(filepath.Join(dir, "a.txt"), []byte(wt), 0o644)
	}
	clashes, err := db.DiffWorktrees(root, "main")
	if err != nil || len(clashes) != 1 || clashes[0].File != "a.txt" {
		t.Fatalf("clashes: %+v %v", clashes, err)
	}
}
