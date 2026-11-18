// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// File is the full project.yaml written when a project is added (§5.3, §8 step 9).
type File struct {
	Name     string `yaml:"name"`
	Root     string `yaml:"root"`
	Language string `yaml:"language"`
	Privacy  struct {
		Mode        string `yaml:"mode"`
		ModelPolicy string `yaml:"model_policy"`
	} `yaml:"privacy"`
	Board struct {
		Kind              string `yaml:"kind"`
		File              string `yaml:"file,omitempty"`
		StaleAfterMinutes int    `yaml:"stale_after_minutes"`
	} `yaml:"board"`
	Worktrees struct {
		Dir string `yaml:"dir"`
	} `yaml:"worktrees"`
	Gates struct {
		DefaultMode   string   `yaml:"default_mode"`
		RedLinesExtra []string `yaml:"red_lines_extra,omitempty"` // "re:<regexp>" entries are enforced by the guard
	} `yaml:"gates"`
	Checks struct {
		Test string `yaml:"test,omitempty"`
	} `yaml:"checks"`
	Ingest struct {
		Depth   string   `yaml:"depth"`
		Include []string `yaml:"include"`
		Exclude []string `yaml:"exclude"`
	} `yaml:"ingest"`
	Budget struct {
		IngestMaxTokens  int `yaml:"ingest_max_tokens"`
		NightlyMaxTokens int `yaml:"nightly_max_tokens"`
	} `yaml:"budget"`
	Delivery string `yaml:"delivery"`
}

// Write creates <home>/hpk/projects/<name>/project.yaml; it refuses to overwrite.
func Write(home string, f File) (*Project, error) {
	if !validName.MatchString(f.Name) {
		return nil, fmt.Errorf("project name %q: use lowercase letters, digits, . _ -", f.Name)
	}
	if err := checkMode(f.Privacy.Mode); err != nil {
		return nil, err
	}
	dir := filepath.Join(ProjectsDir(home), f.Name)
	if _, err := os.Stat(filepath.Join(dir, "project.yaml")); err == nil {
		return nil, fmt.Errorf("project %q already exists", f.Name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	raw, err := marshal2(f)
	if err != nil {
		return nil, err
	}
	head := "# Written by `hpk project add`. Edit freely; see docs/DESIGN.md §5.3.\n"
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), append([]byte(head), raw...), 0o644); err != nil {
		return nil, err
	}
	if err := Audit(dir, "project added", "privacy="+f.Privacy.Mode+" depth="+f.Ingest.Depth); err != nil {
		return nil, err
	}
	return Load(home, f.Name)
}
