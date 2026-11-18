// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package selfinstall puts a downloaded hpk where it belongs: a copy in hpk's
// own folder (<DataDir>/bin), that folder on the user's PATH, and on Windows a
// Start-menu shortcut. A beginner who double-clicks hpk.exe needs nothing else.
// It never touches Hermes or the data folder, and Uninstall reverses it.
package selfinstall

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hpk.local/hpk/internal/launch"
	"hpk.local/hpk/internal/setup"
)

// Result says what Install did.
type Result struct {
	Copied, PathAdded, Shortcut bool
	Target                      string
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "hpk.exe"
	}
	return "hpk"
}

// Dir is the folder that holds the installed hpk.
func Dir() string { return filepath.Join(launch.DataDir(), "bin") }

// PathDirs are the folders put on the user's PATH: hpk's own bin, and the bin
// of the Hermes that hpk installs (it may not exist yet; a PATH entry for a
// missing folder is harmless), so `hermes` works in a new terminal.
func PathDirs() []string {
	return []string{Dir(), setup.BinDir(launch.DataDir())}
}

const marker = "# added by hpk"

// pathFiles are the shell profile files the last addPath changed (Unix only).
var pathFiles []string

// profileLine is the one marked line added to shell profiles.
func profileLine(dirs []string) string {
	return `export PATH="` + strings.Join(dirs, ":") + `:$PATH" ` + marker
}

// Target is where the installed copy lives: <DataDir>/bin/hpk(.exe).
func Target() string { return filepath.Join(Dir(), exeName()) }

// dryRun (HPK_SELFINSTALL_DRYRUN=1) skips the PATH and shortcut changes, so a
// manual try or a test cannot edit the real user's environment.
func dryRun() bool { return os.Getenv("HPK_SELFINSTALL_DRYRUN") == "1" }

func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// IsInstalled reports whether exe is the installed copy itself.
func IsInstalled(exe string) bool { return samePath(exe, Target()) }

func sameBytes(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil || ia.Size() != ib.Size() {
		return false
	}
	x, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	y, err := os.ReadFile(b)
	return err == nil && bytes.Equal(x, y)
}

// stopRunning ends a running hpk (what `hpk stop` does) so its file can be replaced.
func stopRunning(out io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, r := range launch.Runs() {
		if launch.Probe(ctx, r.Addr) != launch.HPK {
			_ = os.Remove(launch.RunFile(r.Addr))
			continue
		}
		if err := launch.Stop(ctx, r); err == nil {
			fmt.Fprintf(out, "Stopped hpk on %s\n", r.Addr)
		}
	}
}

// oldName is where a busy binary is moved aside (Windows cannot overwrite a running exe).
func oldName() string {
	ext := filepath.Ext(exeName())
	return filepath.Join(Dir(), strings.TrimSuffix(exeName(), ext)+".old"+ext)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".hpk-new-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o755); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		// Busy target (Windows): move the old file aside, then put the new one in.
		_ = os.Remove(oldName())
		if err2 := os.Rename(dst, oldName()); err2 != nil {
			os.Remove(name)
			return err
		}
		if err := os.Rename(name, dst); err != nil {
			_ = os.Rename(oldName(), dst)
			os.Remove(name)
			return err
		}
	}
	return nil
}

// Install copies exe to Target(), puts its folder on PATH and, on Windows,
// adds the Start-menu shortcut. It is safe to run again.
func Install(exe string, out io.Writer) (Result, error) {
	res := Result{Target: Target()}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return res, err
	}
	if !samePath(exe, res.Target) && !sameBytes(exe, res.Target) {
		stopRunning(out)
		if err := copyFile(exe, res.Target); err != nil {
			return res, fmt.Errorf("copy hpk to %s: %w", res.Target, err)
		}
		res.Copied = true
		fmt.Fprintf(out, "Copied hpk to %s\n", res.Target)
	}
	if dryRun() {
		return res, nil
	}
	added, err := addPath(PathDirs()...)
	if err != nil {
		// Not fatal: hpk is installed. Say where it is and how to reach it.
		fmt.Fprintf(out, "Could not add %s to your PATH (%v).\n", strings.Join(PathDirs(), " and "), err)
		fmt.Fprintf(out, "hpk is at %s. To use `hpk` and `hermes` in a terminal, add this line to your shell profile:\n  %s\n", res.Target, profileLine(PathDirs()))
	}
	res.PathAdded = added
	if added {
		where := ""
		if len(pathFiles) > 0 {
			where = " in " + strings.Join(pathFiles, ", ")
		}
		fmt.Fprintf(out, "Added %s to your PATH%s (open a new terminal to use `hpk` and `hermes`).\n", strings.Join(PathDirs(), " and "), where)
	}
	sc, err := addShortcut(res.Target)
	if err != nil {
		fmt.Fprintf(out, "Could not make the Start-menu shortcut: %v\n", err)
	}
	res.Shortcut = sc
	return res, nil
}

// Uninstall removes what Install added. Data and the Hermes home stay.
func Uninstall(out io.Writer) error {
	stopRunning(out)
	if !dryRun() {
		removed, err := removePath(PathDirs()...)
		if err != nil {
			return err
		}
		if removed {
			fmt.Fprintf(out, "Removed %s from your PATH.\n", strings.Join(PathDirs(), " and "))
		}
		if err := removeShortcut(); err != nil {
			fmt.Fprintf(out, "Could not remove the Start-menu shortcut: %v\n", err)
		}
		// Start at login points at the file being removed: turn it off.
		if a, err := launch.ThisAutostart(); err == nil && a.Supported() {
			if a.Enabled() {
				if err := a.Disable(); err != nil {
					fmt.Fprintf(out, "Could not turn off start at login: %v\n", err)
				} else {
					fmt.Fprintln(out, "Turned off start at login.")
				}
			}
		}
	}
	for _, f := range []string{Target(), oldName()} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			// A running exe cannot be deleted on Windows: move it aside.
			if err2 := os.Rename(f, f+".gone"); err2 != nil {
				return err
			}
		}
	}
	fmt.Fprintln(out, "hpk was removed. Your projects, wikis and Hermes were left alone.")
	fmt.Fprintln(out, "Reminder: hpk's hooks stay in Hermes' config.yaml unless you ran `hpk uninstall --yes` before this. To take them out now, run `hpk uninstall --yes` with a downloaded hpk.")
	return nil
}
