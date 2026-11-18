// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package selfinstall

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func sandbox(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("HPK_DATA_DIR", filepath.Join(d, "data"))
	t.Setenv("HOME", filepath.Join(d, "home"))
	t.Setenv("USERPROFILE", filepath.Join(d, "home"))
	t.Setenv("APPDATA", filepath.Join(d, "appdata"))
	t.Setenv("HPK_SELFINSTALL_DRYRUN", "1") // never touch the real PATH or Start menu
	if err := os.MkdirAll(filepath.Join(d, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func fakeExe(t *testing.T, d, content string) string {
	t.Helper()
	p := filepath.Join(d, "dl", "hpk-fake")
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTargetUnderDataDir(t *testing.T) {
	d := sandbox(t)
	want := filepath.Join(d, "data", "bin", "hpk")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got := Target(); got != want {
		t.Fatalf("Target = %q, want %q", got, want)
	}
	if IsInstalled(fakeExe(t, d, "x")) {
		t.Fatal("a download is not the installed copy")
	}
}

func TestInstallCopiesAndIsIdempotent(t *testing.T) {
	d := sandbox(t)
	exe := fakeExe(t, d, "v1")
	r, err := Install(exe, io.Discard)
	if err != nil || !r.Copied || r.Target != Target() {
		t.Fatalf("first install: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(Target()); string(b) != "v1" {
		t.Fatalf("target holds %q", b)
	}
	if !IsInstalled(Target()) {
		t.Fatal("IsInstalled(Target()) must be true")
	}
	r2, err := Install(exe, io.Discard)
	if err != nil || r2.Copied {
		t.Fatalf("second install should not copy: %+v %v", r2, err)
	}
}

func TestInstallReplacesOlder(t *testing.T) {
	d := sandbox(t)
	if _, err := Install(fakeExe(t, d, "v1"), io.Discard); err != nil {
		t.Fatal(err)
	}
	r, err := Install(fakeExe(t, d, "v2-longer"), io.Discard)
	if err != nil || !r.Copied {
		t.Fatalf("%+v %v", r, err)
	}
	if b, _ := os.ReadFile(Target()); string(b) != "v2-longer" {
		t.Fatalf("target holds %q", b)
	}
}

func TestProfileLineOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("profile lines are for Unix shells")
	}
	d := sandbox(t)
	home := filepath.Join(d, "home")
	prof := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(prof, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := fakeExe(t, d, "v1")
	for i := 0; i < 2; i++ {
		if _, err := Install(exe, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(prof)
	if n := strings.Count(string(b), "# added by hpk"); n != 1 {
		t.Fatalf("marker appears %d times:\n%s", n, b)
	}
	if _, err := os.Stat(filepath.Join(home, ".bashrc")); err == nil {
		t.Fatal("must not create profiles that do not exist")
	}
}

func TestUninstallRemovesLine(t *testing.T) {
	d := sandbox(t)
	home := filepath.Join(d, "home")
	prof := filepath.Join(home, ".profile")
	if err := os.WriteFile(prof, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(fakeExe(t, d, "v1"), io.Discard); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Uninstall(&out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Target()); err == nil {
		t.Fatal("binary should be gone")
	}
	if runtime.GOOS != "windows" {
		b, _ := os.ReadFile(prof)
		if string(b) != "# mine\n" {
			t.Fatalf("profile after uninstall: %q", b)
		}
	}
	if _, err := os.Stat(filepath.Join(d, "data")); err != nil {
		t.Fatal("data dir must stay")
	}
}
