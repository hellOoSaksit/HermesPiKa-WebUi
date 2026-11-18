// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultRoundTripAndScrub(t *testing.T) {
	home := t.TempDir()
	v, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put(Entry{Name: "early", Kind: "password", Value: "x-long-value", Domains: []string{"a.example"}}); err == nil {
		t.Fatal("no secret before a PIN exists")
	}
	if err := v.SetPIN("", "orchid-garden-42"); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(Entry{Name: "github-login", Kind: "login", User: "mali.dev", Value: "hunter2-very-long", Domains: []string{"https://github.com/login", "*.github.com"}}); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(Entry{Name: "Bad Name", Kind: "password", Value: "x"}); err == nil {
		t.Fatal("bad names are refused")
	}
	raw, _ := os.ReadFile(filepath.Join(home, "hpk", "vault", "vault.bin"))
	if bytes.Contains(raw, []byte("hunter2")) || bytes.Contains(raw, []byte("mali.dev")) {
		t.Fatal("the file is encrypted")
	}
	v2, err := Open(home)
	if err != nil || len(v2.List()) != 1 || v2.List()[0].Domains[0] != "github.com" {
		t.Fatalf("reopen: %v %+v", err, v2.List())
	}
	if pub := v2.List()[0]; pub.Length != 17 || !pub.HasUser {
		t.Fatalf("public view: %+v", pub)
	}
	// filling: only on allowed sites
	if got, err := v2.ForFill("github-login", "value", "github.com"); err != nil || got != "hunter2-very-long" {
		t.Fatalf("fill: %v", err)
	}
	if _, err := v2.ForFill("github-login", "user", "gist.github.com"); err != nil {
		t.Fatal("*.github.com covers subdomains")
	}
	if HostAllowed([]string{"google.com"}, "docs.google.com") {
		t.Fatal("a plain domain is that host only")
	}
	if _, err := v2.ForFill("github-login", "value", "github.com.evil.example"); err == nil {
		t.Fatal("a look-alike site is refused")
	}
	if _, err := v2.ForFill("github-login", "value", "evilgithub.com"); err == nil {
		t.Fatal("a suffix without a dot is refused")
	}
	// scrubbing
	got := v2.Scrub("user mali.dev typed hunter2-very-long; url ?p=hunter2-very-long")
	if strings.Contains(got, "hunter2") || strings.Contains(got, "mali.dev") || !strings.Contains(got, "{{secret:github-login}}") || !strings.Contains(got, "{{secret:github-login.user}}") {
		t.Fatalf("scrub: %s", got)
	}
	for in, want := range map[string]string{"{{secret:github-login}}": "value", "{{secret:github-login.user}}": "user", "github-login.user": "user", "github-login": "value"} {
		if n, p, ok := ParseRef(in); !ok || n != "github-login" || p != want {
			t.Fatalf("ref %s: %s %s %v", in, n, p, ok)
		}
	}
}

func TestEnvFilesAndGuard(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	v, _ := Open(home)
	v.SetPIN("", "orchid-garden-42")
	os.WriteFile(filepath.Join(root, ".env"), []byte("PORT=3000\nAPI_KEY=old\n"), 0o644)
	if err := v.Put(Entry{Name: "stripe", Kind: "env", EnvKey: "API_KEY", Project: "shop", Value: "sk_test_abc 123"}); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(Entry{Name: "db", Kind: "env", EnvKey: "DB_URL", Project: "shop", Value: "postgres://u:p@h/db"}); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(Entry{Name: "x", Kind: "env", EnvKey: "A", Project: "shop", EnvFile: "../.env", Value: "v"}); err == nil {
		t.Fatal("env files stay in the project")
	}
	files, err := v.WriteEnv(home, "shop", root)
	if err != nil || len(files) != 1 {
		t.Fatal(err, files)
	}
	out, _ := os.ReadFile(filepath.Join(root, ".env"))
	s := string(out)
	if !strings.Contains(s, "PORT=3000") || !strings.Contains(s, `API_KEY="sk_test_abc 123"`) || !strings.Contains(s, "DB_URL=postgres://u:p@h/db") || strings.Contains(s, "old") {
		t.Fatalf(".env: %s", s)
	}
	if _, hit := TouchesManaged(home, root, ".env"); !hit {
		t.Fatal("reading .env in the project is caught")
	}
	if _, hit := TouchesManaged(home, root, "cat .env | grep KEY"); !hit {
		t.Fatal("a command on .env is caught")
	}
	if _, hit := TouchesManaged(home, root, filepath.Join(root, ".env")); !hit {
		t.Fatal("an absolute path is caught")
	}
	if _, hit := TouchesManaged(home, root, ".env.example"); hit {
		t.Fatal("another file is fine")
	}
	if _, hit := TouchesManaged(home, t.TempDir(), "cat .env"); hit {
		t.Fatal("another folder's .env is not this one")
	}
}

// The team-server device token is hpk's own: hidden from lists, the AI, pages and the Console.
func TestSystemSecretsAreHidden(t *testing.T) {
	home := t.TempDir()
	v, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	tok := "hpkd_0123456789abcdef0123456789abcdef"
	if err := v.PutSystem("hpk.team-server", tok); err != nil {
		t.Fatal(err)
	}
	if err := v.PutSystem("not-system", tok); err == nil {
		t.Fatal("system names must start with hpk.")
	}
	if len(v.List()) != 0 || len(v.Names()) != 0 || v.Has("hpk.team-server") {
		t.Fatal("system secret is listed")
	}
	if _, _, err := v.Reveal("hpk.team-server"); err == nil {
		t.Fatal("system secret revealed")
	}
	if _, err := v.ForFill("hpk.team-server", "value", "example.com"); err == nil {
		t.Fatal("system secret filled")
	}
	if err := v.Delete("hpk.team-server"); err == nil {
		t.Fatal("system secret deleted from the Console path")
	}
	if err := v.Put(Entry{Name: "hpk.team-server", Kind: "token", Value: "evil"}); err == nil {
		t.Fatal("a person's secret replaced the system one")
	}
	if got := v.Scrub("token " + tok); strings.Contains(got, tok) {
		t.Fatal("system value not scrubbed")
	}
	v2, _ := Open(home)
	if v2.System("hpk.team-server") != tok {
		t.Fatal("system secret lost on reopen")
	}
	if err := v2.DeleteSystem("hpk.team-server"); err != nil || v2.System("hpk.team-server") != "" {
		t.Fatal("DeleteSystem")
	}
}

func TestPINIsSealedAndCounted(t *testing.T) {
	home := t.TempDir()
	v, _ := Open(home)
	if err := v.SetPIN("", "1234"); err == nil {
		t.Fatal("weak PINs are refused")
	}
	if err := v.SetPIN("", "orchid-garden-42"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "hpk", "vault", "pin.json")); err == nil {
		t.Fatal("the PIN lives inside the sealed vault, not in a loose file")
	}
	if err := v.SetPIN("", "hijack-attempt-99"); err == nil {
		t.Fatal("changing the PIN needs the old one")
	}
	for i := 0; i < 5; i++ {
		v.CheckPIN("wrong-guess-000")
	}
	if err := v.CheckPIN("orchid-garden-42"); err == nil || !strings.Contains(err.Error(), "try again") {
		t.Fatalf("locked after 5 wrong tries: %v", err)
	}
	v2, _ := Open(home) // a restart keeps the lock
	if err := v2.CheckPIN("orchid-garden-42"); err == nil {
		t.Fatal("the lock survives a restart")
	}
}

func TestEnvRefusesLinks(t *testing.T) {
	home, root, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	v, _ := Open(home)
	v.SetPIN("", "orchid-garden-42")
	v.Put(Entry{Name: "k", Kind: "env", EnvKey: "API_KEY", Project: "shop", Value: "sk_test_abcdef"})
	if os.Symlink(filepath.Join(elsewhere, "leak.txt"), filepath.Join(root, ".env")) != nil {
		t.Skip("cannot make links here")
	}
	if _, err := v.WriteEnv(home, "shop", root); err == nil {
		t.Fatal("a .env link is refused")
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "leak.txt")); err == nil {
		t.Fatal("nothing was written through the link")
	}
}

func TestScrubForms(t *testing.T) {
	home := t.TempDir()
	v, _ := Open(home)
	v.SetPIN("", "orchid-garden-42")
	v.Put(Entry{Name: "pat", Kind: "token", Value: `github_pat_11ABCDEFG"quote /path+plus x`, Domains: []string{"github.com"}})
	for _, in := range []string{
		`raw github_pat_11ABCDEFG"quote /path+plus x end`,
		`json "github_pat_11ABCDEFG\"quote /path+plus x"`,
		"query q=github_pat_11ABCDEFG%22quote+%2Fpath%2Bplus+x",
		`upper GITHUB_PAT_11ABCDEFG"QUOTE /PATH+PLUS X`,
		"cut github_pat_1…",
		"cut12 github_pat_1",
	} {
		if got := v.Scrub(in); strings.Contains(strings.ToLower(got), "github_pat_1") {
			t.Errorf("leaked: %s → %s", in, got)
		}
	}
}
