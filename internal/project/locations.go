// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package project

import (
	"encoding/json"

	"hpk.local/hpk/internal/isolate"
)

// locations.json records where a person moved a project's wiki to. hpk keeps
// it with the other trusted records (isolate.WriteTrusted): a copy in
// <home>/hpk/trusted for the AI's side to read, and, while the AI is
// isolated, the copy hpk itself reads in the private folder, so a file the AI
// planted cannot make hpk write a wiki somewhere else.
//
//	{"wikis": {"<project>": "<absolute folder>"}}
type locations struct {
	Wikis map[string]string `json:"wikis"`
}

// LocationsFile is the record's name in the trusted folders.
const LocationsFile = "locations.json"

// WikiLocations reads where wikis were moved to (project name -> folder).
func WikiLocations(home string) map[string]string {
	raw, err := isolate.ReadTrusted(home, LocationsFile)
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
	return isolate.WriteTrusted(home, LocationsFile, raw)
}
