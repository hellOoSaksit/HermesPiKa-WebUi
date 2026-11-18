// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package isolate

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// broadSIDs: groups the AI's account belongs to. A write right given to them
// is a write right for the AI.
var broadSIDs = []string{"S-1-1-0", "S-1-5-11", "S-1-5-32-545"} // Everyone, Authenticated Users, Users

// readOnlyDir lets who read and run d, not change it: inherited entries are
// copied once and inheritance is turned off (so a grant on the home does not
// flow in later), then who's own entries are replaced by read and run.
func readOnlyDir(who, d string) error {
	if err := icacls(d, "/inheritance:d"); err != nil {
		return err
	}
	_ = icacls(d, "/remove:g", who)
	return icacls(d, "/grant", who+":(OI)(CI)RX")
}

// writeMask: rights that change a folder or what is in it.
const writeMask = 0x2 | 0x4 | 0x10 | 0x40 | 0x100 | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.GENERIC_WRITE | windows.GENERIC_ALL

// closedTo says why the account who can change dir, or "" when it cannot: dir
// must not inherit, and no entry may give who or a broad group a write right.
func closedTo(dir, who string) string {
	sid, _, _, err := windows.LookupSID("", who)
	if err != nil {
		return "cannot look up the account " + who + ": " + err.Error()
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "cannot read its permissions: " + err.Error()
	}
	ctrl, _, err := sd.Control()
	if err != nil {
		return "cannot read its permissions: " + err.Error()
	}
	if ctrl&windows.SE_DACL_PROTECTED == 0 {
		return "it inherits permissions from the Hermes home, which the AI may change"
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return "it has no access list (anyone may change it)"
	}
	for i := 0; i < int(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(dacl, uint32(i), &ace) != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		if uint32(ace.Mask)&writeMask == 0 {
			continue
		}
		aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if aceSID.Equals(sid) {
			return who + " may change it"
		}
		for _, b := range broadSIDs {
			if aceSID.String() == b {
				return "the group " + b + " (which " + who + " is in) may change it"
			}
		}
	}
	return ""
}

func trustedOpen(c *Config) string {
	dir := TrustedDir(c.Home)
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return closedTo(dir, c.User)
}

// repairTrusted closes <home>/hpk/trusted to the AI's account: read and run
// only, no inherited entries, the broad groups' entries removed, and every
// file in it reset to inherit that and owned by the person (a file the AI made
// there while it could is otherwise still its own to change).
func repairTrusted(c *Config) (bool, error) {
	dir := TrustedDir(c.Home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	if closedTo(dir, c.User) == "" {
		return false, nil
	}
	if err := readOnlyDir(c.User, dir); err != nil {
		return false, err
	}
	args := []string{dir, "/remove:g"}
	for _, b := range broadSIDs {
		args = append(args, "*"+b)
	}
	_ = icacls(args...)
	if ents, _ := os.ReadDir(dir); len(ents) > 0 {
		_ = icacls(filepath.Join(dir, "*"), "/reset", "/T", "/C", "/Q")
	}
	if sid := PersonSID(); sid != "" {
		_ = icacls(dir, "/setowner", "*"+sid, "/T", "/C", "/Q")
	}
	if why := closedTo(dir, c.User); why != "" {
		return true, fmt.Errorf("%s is still open to the AI: %s", dir, why)
	}
	for _, d := range c.ReadOnly {
		if samePath(d, dir) {
			return true, nil
		}
	}
	c.ReadOnly = append(c.ReadOnly, dir) // so teardown lets it inherit again
	return true, Save(c)
}
