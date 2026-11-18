// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package isolate

import (
	"os"
	"path/filepath"
)

func icacls(args ...string) error { return run("icacls", args...) }

// makePrivate creates the private folder with only this account and SYSTEM
// on its access list (no inherited entries).
func makePrivate(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sid := PersonSID()
	if sid == "" {
		return nil
	}
	return icacls(dir, "/inheritance:r", "/grant:r", "*"+sid+":(OI)(CI)F", "*S-1-5-18:(OI)(CI)F")
}

func userSetup(o Options, c *Config) ([]Step, error) {
	pwFile := filepath.Join(o.Private, "ai-account.dpapi")
	who := o.User
	steps := []Step{
		{Say: "Make the private folder " + o.Private + " (only you and SYSTEM; the AI's account cannot open it)", Do: func() error { return makePrivate(o.Private) }},
		{Say: "Create a standard (not administrator) local account " + who + " for Hermes, or give it a new password if it exists; " +
			"hide it from the sign-in screen (HKLM\\" + userListKey + "); its random password is kept for you only in " + pwFile,
			Admin: true, Do: func() error {
				pw := newPassword()
				if err := writePassword(pwFile, pw); err != nil {
					return err
				}
				if Elevated() {
					return CreateAccount(who, pw)
				}
				return RunElevated("ai-account", "create", who, pw)
			}},
	}
	steps = append(steps, moveSteps(o.Home, o.Private, true)...)
	// program folders inside the shared home: the AI may run them, not change
	// them (hpk's hooks binary, Hermes' own code); set before the home grant so
	// that grant does not flow into them
	for _, d := range append([]string{filepath.Dir(o.Bin)}, o.ReadOnly...) {
		d := d
		if _, err := os.Stat(d); err != nil {
			continue
		}
		steps = append(steps, Step{Say: "Let " + who + " read and run, not change, " + d, Do: func() error {
			if err := icacls(d, "/inheritance:d"); err != nil {
				return err
			}
			_ = icacls(d, "/remove:g", who)
			return icacls(d, "/grant", who+":(OI)(CI)RX")
		}})
		c.ReadOnly = append(c.ReadOnly, d)
	}
	steps = append(steps,
		Step{Say: "Let " + who + " read and write the Hermes home " + o.Home, Do: func() error { return icacls(o.Home, "/grant", who+":(OI)(CI)M") }})
	c.Granted = append(c.Granted, o.Home)
	if o.HermesDir != "" {
		if _, err := os.Stat(o.HermesDir); err == nil {
			d := o.HermesDir
			steps = append(steps, Step{Say: "Let " + who + " read and run, not change, Hermes in " + d, Do: func() error { return icacls(d, "/grant", who+":(OI)(CI)RX") }})
			c.Granted = append(c.Granted, d)
		}
	}
	for _, p := range o.Projects {
		p := p
		steps = append(steps, Step{Say: "Let " + who + " read and write the project folder " + p, Do: func() error { return icacls(p, "/grant", who+":(OI)(CI)M") }})
		c.Granted = append(c.Granted, p)
	}
	return steps, nil
}

func userTeardown(c *Config, keepUser bool) []Step {
	who := c.User
	var steps []Step
	for _, p := range c.Granted {
		p := p
		steps = append(steps, Step{Say: "Take " + who + " off " + p, Do: func() error {
			if _, err := os.Stat(p); err != nil {
				return nil
			}
			return icacls(p, "/remove:g", who)
		}})
	}
	for _, d := range c.ReadOnly {
		d := d
		steps = append(steps, Step{Say: "Take " + who + " off " + d + " and let it inherit again", Do: func() error {
			if _, err := os.Stat(d); err != nil {
				return nil
			}
			_ = icacls(d, "/remove:g", who)
			return icacls(d, "/inheritance:e")
		}})
	}
	if !keepUser {
		steps = append(steps, Step{Say: "Delete the local account " + who + " and its sign-in-screen entry (its profile folder C:\\Users\\" + who +
			" stays; remove it in System Properties > User Profiles)", Admin: true, Do: func() error {
			if !AccountExists(who) {
				return nil
			}
			if Elevated() {
				return DeleteAccount(who)
			}
			return RunElevated("ai-account", "delete", who)
		}})
	}
	pw := PasswordFile(c)
	steps = append(steps, Step{Say: "Delete " + pw, Do: func() error {
		if err := os.Remove(pw); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}})
	return steps
}

// UserStatus describes the AI account (Windows).
func UserStatus(c *Config) (exists, hasPassword bool) {
	_, err := os.Stat(PasswordFile(c))
	return AccountExists(c.User), err == nil
}

func grantDir(c *Config, dir string) error {
	return icacls(dir, "/grant", c.User+":(OI)(CI)M")
}

func ungrantDir(c *Config, dir string) error {
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	return icacls(dir, "/remove:g", c.User)
}
