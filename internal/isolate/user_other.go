// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package isolate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// errManual: a dedicated account is automated on Windows only so far;
// docs/isolation.md has the steps for macOS and Linux, or use --docker.
var errManual = errors.New("a separate account is set up by hand on " + runtime.GOOS + " for now (docs/isolation.md); or use hpk isolate setup --docker")

// PasswordFile is unused off Windows (the relay uses sudo).
func PasswordFile(c *Config) string { return filepath.Join(c.Private, "ai-account") }

func makePrivate(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

func userSetup(o Options, c *Config) ([]Step, error) { return nil, errManual }

func userTeardown(c *Config, keepUser bool) []Step { return nil }

// relayUser on macOS/Linux: an account set up by hand per docs/isolation.md,
// with a sudoers rule letting the person run hpk ai-exec as it without a password.
func relayUser(c *Config, s Spec) (int, error) {
	cmd := exec.Command("sudo", "-n", "-u", c.User, "--", c.Bin, "ai-exec", s.Encode())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return 127, err
	}
	return 0, nil
}

// Elevated reports whether this process runs as root.
func Elevated() bool { return os.Geteuid() == 0 }

// AccountExists reports whether a local account exists.
func AccountExists(name string) bool { return exec.Command("id", "-u", name).Run() == nil }

// UserStatus describes the AI account.
func UserStatus(c *Config) (exists, hasPassword bool) { return AccountExists(c.User), true }

// CreateAccount and DeleteAccount are Windows-only.
func CreateAccount(name, pw string) error { return errManual }
func DeleteAccount(name string) error     { return errManual }
func RunElevated(args ...string) error    { return errManual }

func grantDir(c *Config, dir string) error   { return errManual }
func ungrantDir(c *Config, dir string) error { return errManual }

// Outside Windows a separate account is set up by hand (docs/isolation.md).
func trustedOpen(c *Config) string          { return "" }
func repairTrusted(c *Config) (bool, error) { return false, nil }
