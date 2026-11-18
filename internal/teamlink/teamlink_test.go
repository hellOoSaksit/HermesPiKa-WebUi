// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package teamlink

import "testing"

func TestCheckURL(t *testing.T) {
	ok := map[string]string{
		"https://team.example.com/": "https://team.example.com",
		"team.example.com":          "https://team.example.com",
		"https://example.com/hpk":   "https://example.com/hpk",
		"http://127.0.0.1:7878":     "http://127.0.0.1:7878",
		"http://localhost:7878/":    "http://localhost:7878",
	}
	for in, want := range ok {
		if got, err := CheckURL(in); err != nil || got != want {
			t.Errorf("%s: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "http://team.example.com", "ftp://team.example.com", "https://u:p@example.com", "https://team.example.com/?x=1"} {
		if _, err := CheckURL(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestLinkHasNoToken(t *testing.T) {
	home := t.TempDir()
	if l, err := Load(home); l != nil || err != nil {
		t.Fatal("no link yet")
	}
	l := &Link{URL: "https://team.example.com", Member: "max", Role: "member", DeviceID: "dev_1", Device: "pc"}
	if err := l.Save(home); err != nil {
		t.Fatal(err)
	}
	got, err := Load(home)
	if err != nil || got.Member != "max" {
		t.Fatal(err)
	}
	if err := Remove(home); err != nil {
		t.Fatal(err)
	}
	if err := Remove(home); err != nil {
		t.Fatal("removing twice")
	}
}
