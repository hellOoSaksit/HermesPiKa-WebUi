// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package isolate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// While isolated, hpk reads only its own copy; the AI's copy in the home is
// written too but never believed.
func TestTrustedCopies(t *testing.T) {
	setConfig(t)
	home, priv := t.TempDir(), t.TempDir()
	// not isolated: one folder, in the home
	if err := WriteTrusted(home, "x.json", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if raw, _ := ReadTrusted(home, "x.json"); string(raw) != "one" || OwnTrustedDir(home) != TrustedDir(home) {
		t.Fatalf("%q", raw)
	}
	Save(&Config{Mode: ModeUser, User: "hpk-ai", Home: home, Private: priv})
	if OwnTrustedDir(home) != filepath.Join(priv, "trusted") {
		t.Fatal(OwnTrustedDir(home))
	}
	if err := WriteTrusted(home, "x.json", []byte("two")); err != nil {
		t.Fatal(err)
	}
	shared, _ := os.ReadFile(filepath.Join(TrustedDir(home), "x.json"))
	if string(shared) != "two" {
		t.Fatalf("the AI's copy is %q", shared)
	}
	// the AI rewrites its copy: hpk does not see it
	os.WriteFile(filepath.Join(TrustedDir(home), "x.json"), []byte("forged"), 0o644)
	if raw, _ := ReadTrusted(home, "x.json"); string(raw) != "two" {
		t.Fatalf("hpk read %q", raw)
	}
}

func TestComposeMountsTrustedReadOnly(t *testing.T) {
	setConfig(t)
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "compose.yaml")
	_, c, err := SetupPlan(Options{Mode: ModeDocker, Home: home, Private: t.TempDir(), Compose: file})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCompose(file, "img", "box", c.Mounts); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if !strings.Contains(string(raw), ":"+ContainerHome+"/hpk/trusted:ro\"") {
		t.Fatalf("compose:\n%s", raw)
	}
	if trustedFinding(c) != "" {
		t.Fatal("a new docker setup is reported open")
	}
	old := &Config{Mode: ModeDocker, Home: home, Mounts: []Mount{{Host: home, Container: ContainerHome}}}
	if trustedFinding(old) == "" {
		t.Fatal("an old docker setup (home read-write, trusted inside) is not reported")
	}
}

// Run as the same account, the probe can write <home>/hpk/trusted: Judge says so.
func TestProbeSeesWritableTrusted(t *testing.T) {
	setConfig(t)
	home, priv := t.TempDir(), t.TempDir()
	os.MkdirAll(TrustedDir(home), 0o755)
	os.WriteFile(filepath.Join(TrustedDir(home), "locations.json"), []byte("{}"), 0o644)
	r := Probe(priv, home)
	if len(r.TrustedWritten) != 2 {
		t.Fatalf("probe: %+v", r)
	}
	bad := strings.Join(Judge(&Config{Mode: ModeDocker, Home: home, Private: priv}, r), "\n")
	if !strings.Contains(bad, "can change "+TrustedDir(home)) {
		t.Fatalf("judge: %s", bad)
	}
	if ents, _ := os.ReadDir(TrustedDir(home)); len(ents) != 1 {
		t.Fatal("the probe left a file behind")
	}
}

func TestMoveStepsCopyTrusted(t *testing.T) {
	setConfig(t)
	home, priv := t.TempDir(), filepath.Join(t.TempDir(), "private")
	os.MkdirAll(TrustedDir(home), 0o755)
	os.WriteFile(filepath.Join(TrustedDir(home), "folders-app.sha256"), []byte("abc\n"), 0o644)
	if err := moveSteps(home, priv, true)[0].Do(); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(priv, "trusted", "folders-app.sha256")); string(raw) != "abc\n" {
		t.Fatalf("not copied: %q", raw)
	}
	// back: hpk's copy wins over what the AI may have written meanwhile
	os.WriteFile(filepath.Join(TrustedDir(home), "folders-app.sha256"), []byte("forged\n"), 0o644)
	if err := moveSteps(home, priv, false)[0].Do(); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(TrustedDir(home), "folders-app.sha256")); string(raw) != "abc\n" {
		t.Fatalf("not copied back: %q", raw)
	}
}

// A recordings folder outside the home and projects stays put, and setup
// resets its permissions; a failure there fails the step.
func TestRecordingsOutsideMadePrivate(t *testing.T) {
	setConfig(t)
	home, rec := t.TempDir(), t.TempDir()
	var got string
	makePrivateDir = func(dir string) error { got = dir; return errors.New("no rights") }
	t.Cleanup(func() { makePrivateDir = makePrivate })
	st := recordingsStep(Options{Home: home, Private: t.TempDir(), Recordings: rec, ResetRecordings: func() error { return nil }})
	if st == nil || !strings.Contains(st.Say, rec) {
		t.Fatalf("step: %+v", st)
	}
	if err := st.Do(); err == nil || got != rec {
		t.Fatalf("makePrivate(%q) err=%v", got, err)
	}
}
