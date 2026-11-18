// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package project

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// locations.json lives in <home>/hpk/trusted, which the AI's account can read
// but not change: where a person moved a project's wiki to.
//
//	{"wikis": {"<project>": "<absolute folder>"}}
type locations struct {
	Wikis map[string]string `json:"wikis"`
}

func locationsFile(home string) string {
	return filepath.Join(home, "hpk", "trusted", "locations.json")
}

// WikiLocations reads where wikis were moved to (project name -> folder).
func WikiLocations(home string) map[string]string {
	raw, err := os.ReadFile(locationsFile(home))
	if err != nil {
		return map[string]string{}
	}
	var l locations
	if json.Unmarshal(raw, &l) != nil || l.Wikis == nil {
		return map[string]string{}
	}
	return l.Wikis
}

// SetWikiLocation records that project name's wiki now lives in dir; an empty
// dir puts it back in the project's own folder.
func SetWikiLocation(home, name, dir string) error {
	l := locations{Wikis: WikiLocations(home)}
	if dir == "" {
		delete(l.Wikis, name)
	} else {
		l.Wikis[name] = dir
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	f := locationsFile(home)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}
