// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package isolate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setConfig(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "isolation.json")
	t.Setenv("HPK_ISOLATION_FILE", f)
	return f
}

func TestNotIsolatedKeepsTheOldLayout(t *testing.T) {
	setConfig(t)
	home := t.TempDir()
	if Active() {
		t.Fatal("active without a config")
	}
	if got := PrivateDir(home); got != filepath.Join(home, "hpk") {
		t.Fatalf("PrivateDir = %s", got)
	}
	if fs := Findings(home); len(fs) != 1 || fs[0].Level != "warn" && fs[0].Level != "info" {
		t.Fatalf("findings: %+v", fs)
	}
	if cmd := Command("hermes", "--version"); filepath.Base(cmd.Path) == "ai-relay" || len(cmd.Args) != 2 {
		t.Fatalf("not isolated, hermes runs directly: %v", cmd.Args)
	}
}

func TestIsolatedPrivateDirOnlyForItsHome(t *testing.T) {
	setConfig(t)
	home, priv := t.TempDir(), t.TempDir()
	if err := Save(&Config{Mode: ModeUser, User: "hpk-ai", Home: home, Private: priv}); err != nil {
		t.Fatal(err)
	}
	if !Active() {
		t.Fatal("not active")
	}
	if got := PrivateDir(home); got != priv {
		t.Fatalf("PrivateDir = %s, want %s", got, priv)
	}
	other := t.TempDir()
	if got := PrivateDir(other); got != filepath.Join(other, "hpk") {
		t.Fatalf("another home must keep its own layout: %s", got)
	}
	cmd := Command("hermes-acp")
	if len(cmd.Args) < 3 || cmd.Args[1] != "ai-relay" || cmd.Args[3] != "hermes-acp" {
		t.Fatalf("isolated, hermes goes through the relay: %v", cmd.Args)
	}
}

func TestBrokenConfigIsNotIsolation(t *testing.T) {
	f := setConfig(t)
	os.WriteFile(f, []byte(`{"mode":"sandbox","home":"x","private":"y"}`), 0o600)
	if Active() {
		t.Fatal("unknown mode accepted")
	}
}

func TestSpecRoundTripAndAllowList(t *testing.T) {
	s := Spec{Name: "hermes-acp", Args: []string{"--x", "a b"}, Dir: `C:\code\app`, Env: []string{"HPK_PROJECT=app"}}
	got, err := Decode(s.Encode())
	if err != nil || got.Name != s.Name || got.Args[1] != "a b" || got.Dir != s.Dir || got.Env[0] != s.Env[0] {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if _, err := Decode("!!"); err == nil {
		t.Fatal("bad spec accepted")
	}
	for _, ok := range []string{"hermes", "hermes.exe", `C:\x\hermes-acp.exe`, "/usr/bin/hermes"} {
		if !Allowed(ok) {
			t.Errorf("%s refused", ok)
		}
	}
	for _, bad := range []string{"cmd", "powershell.exe", "sh", "hermes2", "python"} {
		if Allowed(bad) {
			t.Errorf("%s allowed", bad)
		}
	}
	if code := Exec(Spec{Name: "powershell"}); code != 126 {
		t.Fatalf("ai-exec ran another program: %d", code)
	}
}

func TestNewSpecForwardsOnlyHermesVariables(t *testing.T) {
	t.Setenv("HPK_PROJECT", "app")
	t.Setenv("HERMES_HOME", "/somewhere/else")
	t.Setenv("OPENAI_API_KEY", "sk-should-not-cross")
	s := NewSpec("/shared/home", "hermes", nil)
	env := strings.Join(s.Env, "\n")
	if !strings.Contains(env, "HPK_PROJECT=app") || !strings.Contains(env, "HERMES_HOME=/shared/home") || !strings.Contains(env, "HPK_HOME=/shared/home") {
		t.Fatalf("env: %s", env)
	}
	if strings.Contains(env, "sk-should") || strings.Contains(env, "/somewhere/else") {
		t.Fatalf("leaked: %s", env)
	}
}

func TestDockerArgsMapPaths(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"h", "hermes")
	proj := filepath.Join(string(filepath.Separator)+"code", "app")
	if runtime.GOOS == "windows" {
		home, proj = `C:\Users\p\AppData\Local\hermes`, `D:\code\app`
	}
	c := &Config{Mode: ModeDocker, Container: "hpk-ai", Home: home,
		Mounts: []Mount{{Host: home, Container: ContainerHome}, {Host: proj, Container: ContainerPath(proj)}}}
	s := Spec{Name: "hermes-acp", Args: []string{"--x"}, Dir: filepath.Join(proj, "src"),
		Env: []string{"PATH=/x", "HERMES_HOME=" + home, "WIKI_PATH=" + filepath.Join(home, "hpk", "projects", "app", "wiki"), "HPK_PROJECT=app"}}
	got := strings.Join(DockerArgs(c, s), " ")
	wantDir := ContainerPath(proj) + "/src"
	for _, want := range []string{"exec -i -u hermes", "-w " + wantDir, "-e HERMES_HOME=/opt/data", "-e WIKI_PATH=/opt/data/hpk/projects/app/wiki", "-e HPK_PROJECT=app", "hpk-ai hermes-acp --x"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing in %s", want, got)
		}
	}
	if strings.Contains(got, "PATH=/x") {
		t.Errorf("the host PATH crossed: %s", got)
	}
	if runtime.GOOS == "windows" && ContainerPath(`D:\code\app`) != "/host/d/code/app" {
		t.Errorf("ContainerPath = %s", ContainerPath(`D:\code\app`))
	}
}

func TestSetupPlanRules(t *testing.T) {
	setConfig(t)
	home := t.TempDir()
	if _, _, err := SetupPlan(Options{Mode: ModeDocker, Home: home, Private: filepath.Join(home, "hpk", "private")}); err == nil {
		t.Fatal("a private folder inside the Hermes home was accepted")
	}
	if _, _, err := SetupPlan(Options{Mode: ModeUser, Home: home, Private: t.TempDir(), Bin: filepath.Join(home, "nope")}); err == nil {
		t.Fatal("setup without hpk installed into Hermes")
	}
	steps, c, err := SetupPlan(Options{Mode: ModeDocker, Home: home, Private: t.TempDir(), Projects: []string{t.TempDir()}, Compose: filepath.Join(t.TempDir(), "compose.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Mounts) != 3 || c.Container != DefaultUser || !c.Mounts[1].ReadOnly || c.Mounts[1].Host != TrustedDir(home) {
		t.Fatalf("config: %+v", c)
	}
	var said []string
	for _, s := range steps {
		said = append(said, s.Say)
	}
	all := strings.Join(said, "\n")
	for _, want := range []string{"private folder", "Move the vault", "compose", "up -d", "Save these settings"} {
		if !strings.Contains(all, want) {
			t.Errorf("plan misses %q:\n%s", want, all)
		}
	}
}

func TestMoveAndBackAndLeftovers(t *testing.T) {
	setConfig(t)
	home, priv := t.TempDir(), filepath.Join(t.TempDir(), "private")
	for _, rel := range []string{"vault/vault.bin", "vault/key.dpapi", "browser/profile/Default/Cookies", "console-sessions.json", "console-link", "vault/managed-env.json", "browser/agent.json"} {
		p := filepath.Join(home, "hpk", filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte(rel), 0o600)
	}
	if len(Leftovers(home)) == 0 {
		t.Fatal("no leftovers seen")
	}
	if err := makePrivate(priv); err != nil {
		t.Fatal(err)
	}
	if err := moveSteps(home, priv, true)[0].Do(); err != nil {
		t.Fatal(err)
	}
	if l := Leftovers(home); len(l) != 0 {
		t.Fatalf("still in the home: %v", l)
	}
	for _, rel := range []string{"vault/vault.bin", "vault/key.dpapi", "browser/profile/Default/Cookies", "console-sessions.json"} {
		if _, err := os.Stat(filepath.Join(priv, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s not moved: %v", rel, err)
		}
	}
	// what the AI side needs stays in the shared home
	for _, rel := range []string{"vault/managed-env.json", "browser/agent.json"} {
		if _, err := os.Stat(filepath.Join(home, "hpk", filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s left the home: %v", rel, err)
		}
	}
	if err := moveSteps(home, priv, false)[0].Do(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "hpk", "vault", "vault.bin")); err != nil {
		t.Fatal("not moved back")
	}
}

func TestFindingsSeeLeftoversAndJudgeSeesOpenPrivate(t *testing.T) {
	setConfig(t)
	home, priv := t.TempDir(), t.TempDir()
	Save(&Config{Mode: ModeDocker, Container: "hpk-ai-test-none", Home: home, Private: priv})
	os.MkdirAll(filepath.Join(home, "hpk", "vault"), 0o700)
	os.WriteFile(filepath.Join(home, "hpk", "vault", "vault.bin"), []byte("x"), 0o600)
	var names []string
	for _, f := range Findings(home) {
		names = append(names, f.Level+":"+f.Name)
	}
	if !strings.Contains(strings.Join(names, " "), "warn:private files") {
		t.Fatalf("findings: %v", names)
	}
	// run as the same account, the probe can open the private folder: Judge must say so
	r := Probe(priv, home)
	if !r.HomeWritable || len(r.PrivateRead) == 0 {
		t.Fatalf("probe: %+v", r)
	}
	c := &Config{Mode: ModeUser, User: "hpk-ai", Home: home, Private: priv}
	if bad := Judge(c, r); len(bad) < 2 {
		t.Fatalf("judge: %v", bad)
	}
	if bad := Judge(c, ProbeResult{User: `PC\hpk-ai`, HomeWritable: true}); len(bad) != 0 {
		t.Fatalf("a clean probe: %v", bad)
	}
}

// A spec longer than Windows' 1024-character logon command line goes in a
// one-time file: read once, then gone; nothing outside hpk/relay is read.
func TestLongSpecGoesThroughAOneTimeFile(t *testing.T) {
	home := t.TempDir()
	s := Spec{Name: "hermes", Args: []string{"--version"}, Env: []string{"PATH=" + strings.Repeat(`C:\tools;`, 300)}}
	f, err := WriteSpecFile(home, s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadSpecArg("@" + f)
	if err != nil || got.Name != "hermes" || got.Env[0] != s.Env[0] {
		t.Fatalf("read back: %+v %v", got, err)
	}
	if _, err := os.Stat(f); err == nil {
		t.Fatal("the spec file is deleted once read")
	}
	if _, err := ReadSpecArg("@" + f); err == nil {
		t.Fatal("a spec file is read only once")
	}
	other := filepath.Join(home, "elsewhere.txt")
	os.WriteFile(other, []byte(s.Encode()), 0o600)
	if _, err := ReadSpecArg("@" + other); err == nil {
		t.Fatal("only hpk/relay/spec-* files are read")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("a refused file is left alone")
	}
	if got, err := ReadSpecArg(s.Encode()); err != nil || got.Name != "hermes" {
		t.Fatal("an inline spec still works")
	}
}
