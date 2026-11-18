// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package isolate

// RepairTrusted makes sure the AI's account cannot change <home>/hpk/trusted
// (separate-account mode on Windows; setups made before that folder existed
// let it inherit the home's read-write grant). It reports whether it had to
// change anything. hpk serve runs it at start, hpk isolate status too.
func RepairTrusted(c *Config) (bool, error) {
	if c == nil || c.Mode != ModeUser {
		return false, nil
	}
	return repairTrusted(c)
}

// trustedFinding says what is wrong with <home>/hpk/trusted for the AI, or "".
func trustedFinding(c *Config) string {
	if err := sharedGuard(c.Home); err != nil {
		return err.Error()
	}
	switch c.Mode {
	case ModeUser:
		return trustedOpen(c)
	case ModeDocker:
		for _, m := range c.Mounts {
			if m.ReadOnly && samePath(m.Host, TrustedDir(c.Home)) {
				return ""
			}
		}
		return "the container mounts it read-write with the home (set up before it was read-only)"
	}
	return ""
}
