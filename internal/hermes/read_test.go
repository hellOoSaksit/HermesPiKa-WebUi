// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package hermes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAuthList(t *testing.T) {
	out := "anthropic (2 credentials):\n" +
		"  #1  Work account         oauth   id=aa11 priority=0 hermes_pkce ←\n" +
		"  #2  Personal             oauth   id=bb22 priority=1 hermes_pkce exhausted until 14:00\n\n" +
		"openrouter (1 credential):\n" +
		"  #1  team key             api_key id=cc33 priority=0 manual\n\n" +
		"nous: free tier\n  hint line\n"
	got := ParseAuthList(out)
	if len(got) != 3 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	if got[0].Label != "Work account" || !got[0].Current || got[0].Source != "hermes_pkce" {
		t.Errorf("row 1: %+v", got[0])
	}
	if got[1].Status != "exhausted until 14:00" || got[1].Priority != 1 || got[1].Current {
		t.Errorf("row 2: %+v", got[1])
	}
	if got[2].Provider != "openrouter" || got[2].Type != "api_key" {
		t.Errorf("row 3: %+v", got[2])
	}
}

func TestProfiles(t *testing.T) {
	h := t.TempDir()
	os.WriteFile(filepath.Join(h, "config.yaml"), []byte("model:\n  default: \"m-big\"\n"), 0o600)
	os.MkdirAll(filepath.Join(h, "profiles", "b-work"), 0o755)
	os.MkdirAll(filepath.Join(h, "profiles", "a-home"), 0o755)
	os.WriteFile(filepath.Join(h, "profiles", "a-home", "config.yaml"), []byte("model: m-small\n"), 0o600)
	ps := Profiles(h)
	if len(ps) != 3 || ps[0].Model != "m-big" || !ps[0].Default || ps[1].Name != "a-home" || ps[1].Model != "m-small" || ps[2].Model != "" {
		t.Fatalf("%+v", ps)
	}
}
