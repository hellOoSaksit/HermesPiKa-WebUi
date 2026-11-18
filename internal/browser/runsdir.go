// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package browser

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"hpk.local/hpk/internal/launch"
)

func settingsFile() string { return filepath.Join(launch.DataDir(), "settings.json") }

func readSettings() map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(settingsFile()); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// RunsDir is where recordings are kept: the folder in hpk's settings.json
// ("recordings"), else <browser dir>/runs.
func RunsDir(home string) string {
	var d string
	if raw, ok := readSettings()["recordings"]; ok && json.Unmarshal(raw, &d) == nil && filepath.IsAbs(d) {
		return filepath.Clean(d)
	}
	return filepath.Join(Dir(home), "runs")
}

// SetRunsDir records a new recordings folder; "" goes back to the default.
// Other settings in the file are kept.
func SetRunsDir(dir string) error {
	if dir != "" && !filepath.IsAbs(dir) {
		return errors.New("give the folder's full path")
	}
	m := readSettings()
	if dir == "" {
		delete(m, "recordings")
	} else {
		raw, _ := json.Marshal(filepath.Clean(dir))
		m["recordings"] = raw
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsFile()), 0o700); err != nil {
		return err
	}
	tmp := settingsFile() + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, settingsFile())
}

// MarkRunning flags the browser as running without starting one; for tests of
// code that must refuse while it runs.
func (m *Manager) MarkRunning(on bool) { m.setState(func(s *State) { s.Running = on }) }
