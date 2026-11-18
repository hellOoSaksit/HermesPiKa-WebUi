// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package teamlink

import (
	"os"
	"path/filepath"
	"testing"
)

// The device token travels only to the server it was issued by.
func TestCredentialBindsTheServer(t *testing.T) {
	c := Credential{URL: "https://team.example.com", Token: "hpkd_abc"}
	got, err := DecodeCredential(c.Encode())
	if err != nil || got != c {
		t.Fatalf("round trip: %v %+v", err, got)
	}
	for _, bad := range []string{"", "hpkd_bare_token_from_before", `{"url":"http://evil.example","token":"hpkd_x"}`, `{"url":"https://team.example.com/","token":""}`} {
		if _, err := DecodeCredential(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	l := &Link{URL: "https://team.example.com"}
	if err := l.Bound(c); err != nil {
		t.Fatal(err)
	}
	moved := &Link{URL: "https://attacker.example"}
	if moved.Bound(c) == nil {
		t.Fatal("a rewritten teamserver.json would receive the token")
	}
}

// teamserver.json goes through the same URL rules as joining.
func TestLoadChecksTheURL(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "hpk"), 0o755)
	os.WriteFile(File(home), []byte(`{"url":"http://evil.example:9999"}`), 0o600)
	if _, err := Load(home); err == nil {
		t.Fatal("plain http to another host is refused")
	}
	os.WriteFile(File(home), []byte(`{"url":"team.example.com/"}`), 0o600)
	l, err := Load(home)
	if err != nil || l.URL != "https://team.example.com" {
		t.Fatalf("normalised: %v %+v", err, l)
	}
}
