// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package isolate

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

// A setup made before hpk/trusted existed: the folder inherits the home's
// read-write grant. RepairTrusted closes it and says so once.
func TestRepairTrustedClosesInheritedGrant(t *testing.T) {
	setConfig(t)
	const who = "Guest" // a built-in account that exists everywhere, standing in for hpk-ai
	if _, _, _, err := windows.LookupSID("", who); err != nil {
		t.Skip("no Guest account here")
	}
	home, priv := t.TempDir(), t.TempDir()
	if out, err := exec.Command("icacls", home, "/grant", who+":(OI)(CI)M").CombinedOutput(); err != nil {
		t.Skipf("icacls: %v %s", err, out)
	}
	c := &Config{Mode: ModeUser, User: who, Home: home, Private: priv}
	Save(c)
	fixed, err := RepairTrusted(c)
	if err != nil || !fixed {
		t.Fatalf("fixed=%v err=%v", fixed, err)
	}
	if why := trustedFinding(c); why != "" {
		t.Fatalf("still open: %s", why)
	}
	if fixed, err := RepairTrusted(c); err != nil || fixed {
		t.Fatalf("second run: fixed=%v err=%v", fixed, err)
	}
	if len(c.ReadOnly) != 1 {
		t.Fatalf("teardown will not know: %v", c.ReadOnly)
	}
}
