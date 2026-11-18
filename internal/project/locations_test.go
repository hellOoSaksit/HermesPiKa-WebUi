// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package project

import (
	"path/filepath"
	"testing"
)

func TestWikiDirOverride(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "code", "orbit-app")
	if _, err := Create(home, "orbit-app", root, Shared); err != nil {
		t.Fatal(err)
	}
	p, _ := Load(home, "orbit-app")
	if p.WikiDir() != filepath.Join(p.Dir, "wiki") {
		t.Fatalf("default: %s", p.WikiDir())
	}
	want := filepath.Join(home, "elsewhere", "orbit-wiki")
	if err := SetWikiLocation(home, "orbit-app", want); err != nil {
		t.Fatal(err)
	}
	p, _ = Load(home, "orbit-app")
	if p.WikiDir() != want {
		t.Fatalf("got %s", p.WikiDir())
	}
	SetWikiLocation(home, "orbit-app", "")
	p, _ = Load(home, "orbit-app")
	if p.WikiDir() != filepath.Join(p.Dir, "wiki") {
		t.Fatalf("reset: %s", p.WikiDir())
	}
}
