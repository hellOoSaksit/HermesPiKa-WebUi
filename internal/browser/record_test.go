// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hpk.local/hpk/internal/isolate"
)

func TestRunsDirSetting(t *testing.T) {
	data, home := t.TempDir(), t.TempDir()
	t.Setenv("HPK_DATA_DIR", data)
	t.Setenv("HPK_ISOLATION_FILE", filepath.Join(t.TempDir(), "none.json"))
	def := filepath.Join(home, "hpk", "browser", "runs")
	if got := RunsDir(home); got != def {
		t.Fatalf("default %s", got)
	}
	os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"other":"keep"}`), 0o600)
	custom := filepath.Join(t.TempDir(), "my-runs")
	if err := SetRunsDir(custom); err != nil {
		t.Fatal(err)
	}
	if got := RunsDir(home); got != custom {
		t.Fatalf("custom %s", got)
	}
	var m map[string]string
	raw, _ := os.ReadFile(filepath.Join(data, "settings.json"))
	json.Unmarshal(raw, &m)
	if m["other"] != "keep" || m["recordings"] != custom {
		t.Errorf("settings: %v", m)
	}
	if err := SetRunsDir("relative"); err == nil {
		t.Error("relative dir accepted")
	}
	if err := SetRunsDir(""); err != nil {
		t.Fatal(err)
	}
	if got := RunsDir(home); got != def {
		t.Fatalf("reset %s", got)
	}
	raw, _ = os.ReadFile(filepath.Join(data, "settings.json"))
	if !strings.Contains(string(raw), "keep") || strings.Contains(string(raw), "recordings") {
		t.Errorf("after reset: %s", raw)
	}
}

func TestIsolateSetupTakesBackReadableRecordings(t *testing.T) {
	t.Setenv("HPK_DATA_DIR", t.TempDir())
	t.Setenv("HPK_ISOLATION_FILE", filepath.Join(t.TempDir(), "isolation.json"))
	home, priv, proj := t.TempDir(), filepath.Join(t.TempDir(), "private"), t.TempDir()
	rec := filepath.Join(proj, "recordings")
	id := "20260101-120000-abcdef"
	os.MkdirAll(filepath.Join(rec, id), 0o700)
	os.WriteFile(filepath.Join(rec, id, "run.json"), []byte(`{"id":"`+id+`"}`), 0o600)
	if err := SetRunsDir(rec); err != nil {
		t.Fatal(err)
	}
	steps, _, err := isolate.SetupPlan(isolate.Options{Mode: isolate.ModeDocker, Home: home, Private: priv, Projects: []string{proj},
		Compose: filepath.Join(t.TempDir(), "c.yaml"), Recordings: RunsDir(home), ResetRecordings: func() error { return SetRunsDir("") }})
	if err != nil {
		t.Fatal(err)
	}
	var back *isolate.Step
	for i := range steps {
		if strings.Contains(steps[i].Say, "browser recordings") {
			back = &steps[i]
		}
	}
	if back == nil {
		t.Fatal("no step for the browser recordings")
	}
	if err := back.Do(); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(priv, "browser", "runs")
	if _, err := os.Stat(filepath.Join(want, id, "run.json")); err != nil {
		t.Fatalf("recording not in the private folder: %v", err)
	}
	if _, err := os.Stat(rec); err == nil {
		t.Error("old folder left")
	}
	// the private default applies once isolation is saved
	raw, _ := json.Marshal(map[string]string{"mode": "docker", "home": home, "private": priv})
	os.WriteFile(os.Getenv("HPK_ISOLATION_FILE"), raw, 0o600)
	if got := RunsDir(home); got != want {
		t.Errorf("RunsDir %s, want %s", got, want)
	}

	// a folder outside the home and projects is left alone
	other := t.TempDir()
	SetRunsDir(other)
	steps, _, _ = isolate.SetupPlan(isolate.Options{Mode: isolate.ModeDocker, Home: home, Private: priv, Projects: []string{proj},
		Compose: filepath.Join(t.TempDir(), "c.yaml"), Recordings: RunsDir(home), ResetRecordings: func() error { return SetRunsDir("") }})
	for _, s := range steps {
		if strings.Contains(s.Say, "browser recordings") {
			t.Errorf("a private custom folder was moved: %s", s.Say)
		}
	}
}
