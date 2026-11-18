// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package isolate

import (
	"os"
	"os/exec"
	"path/filepath"
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

func icaclsOut(t *testing.T, p string) string {
	t.Helper()
	out, err := exec.Command("icacls", p).CombinedOutput()
	if err != nil {
		t.Fatalf("icacls %s: %v %s", p, err, out)
	}
	return string(out)
}

// The AI recreates <home>/hpk with "trusted" as a junction to a folder of its
// choosing: repair, trusted writes and the teardown copy must not touch it.
func TestTrustedJunctionRefused(t *testing.T) {
	setConfig(t)
	home, priv, target := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(target, "victim.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(home, "hpk"), 0o755)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", TrustedDir(home), target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	beforeDir, beforeFile := icaclsOut(t, target), icaclsOut(t, filepath.Join(target, "victim.txt"))
	c := &Config{Mode: ModeUser, User: "Guest", Home: home, Private: priv}
	Save(c)
	if _, err := RepairTrusted(c); err == nil {
		t.Fatal("repair went through a junction")
	}
	if icaclsOut(t, target) != beforeDir || icaclsOut(t, filepath.Join(target, "victim.txt")) != beforeFile {
		t.Fatal("the junction target's permissions changed")
	}
	if trustedFinding(c) == "" {
		t.Fatal("status does not report the junction")
	}
	if err := WriteTrusted(home, "x.json", []byte("{}")); err == nil {
		t.Fatal("wrote through the junction")
	}
	os.MkdirAll(filepath.Join(priv, "trusted"), 0o755)
	os.WriteFile(filepath.Join(priv, "trusted", "y.json"), []byte("{}"), 0o644)
	if err := moveSteps(home, priv, false)[0].Do(); err == nil {
		t.Fatal("teardown copied through the junction")
	}
	if ents, _ := os.ReadDir(target); len(ents) != 1 {
		t.Fatalf("files written into the target: %v", ents)
	}
}
