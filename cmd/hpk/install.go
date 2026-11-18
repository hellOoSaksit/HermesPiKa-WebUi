// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/install"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/version"
)

// runInstall handles `hpk install` and `hpk uninstall`. Both show the exact change
// to Hermes' config.yaml and write only with --yes from a person.
func runInstall(remove bool, args []string, stdout, stderr io.Writer) int {
	name := "install"
	if remove {
		name = "uninstall"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	home := fs.String("home", "", "Hermes home (default: $HERMES_HOME or the OS default)")
	profile := fs.String("profile", "", "Hermes profile to change (default: the default profile)")
	yes := fs.Bool("yes", false, "a person agrees to the change shown")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	h := *home
	if h == "" {
		h = project.Home()
	}
	paths := install.Resolve(h, *profile)
	var pl install.Plan
	var err error
	if remove {
		pl, err = install.PlanUninstall(paths)
	} else {
		pl, err = install.PlanInstall(paths)
	}
	if err != nil {
		fmt.Fprintln(stderr, "hpk "+name+":", err)
		return 1
	}
	if *asJSON && !*yes {
		printJSON(stdout, pl)
		return exitNeedsConsent
	}
	if !*yes {
		fmt.Fprintf(stdout, "hpk %s would change %s:\n\n", name, pl.Paths.Config)
		for _, c := range pl.Changes {
			fmt.Fprintln(stdout, "  •", c)
		}
		if len(pl.Diff) > 0 {
			fmt.Fprintln(stdout, "\nDiff:")
			for _, l := range pl.Diff {
				fmt.Fprintln(stdout, "  "+l)
			}
		} else {
			fmt.Fprintln(stdout, "\n(config.yaml is already up to date)")
		}
		if !remove {
			fmt.Fprintf(stdout, "\nIt also copies hpk to %s and its skills to %s.\n", pl.Paths.Bin, pl.Paths.SkillsDir)
			fmt.Fprintln(stdout, "A backup of config.yaml is kept next to it. Your projects and wikis are not touched.")
			fmt.Fprintln(stdout, "Hermes asks you once to allow each new hook the first time it runs (or set hooks_auto_accept).")
		} else {
			fmt.Fprintln(stdout, "\nProjects, wikis and board data stay. The hpk binary stays where it is.")
		}
		fmt.Fprintf(stdout, "\nTo go ahead: hpk %s --yes%s\n", name, profileFlag(*profile))
		return exitNeedsConsent
	}
	now := time.Now()
	if remove {
		backup, err := install.ApplyUninstall(pl, now)
		if err != nil {
			fmt.Fprintln(stderr, "hpk uninstall:", err)
			return 1
		}
		fmt.Fprintf(stdout, "hpk removed from %s", pl.Paths.Config)
		if backup != "" {
			fmt.Fprintf(stdout, " (backup: %s)", backup)
		}
		fmt.Fprintln(stdout)
		return 0
	}
	self, _ := os.Executable()
	m, err := install.Apply(pl, self, assets.Skills(), version.Version, now)
	if err != nil {
		fmt.Fprintln(stderr, "hpk install:", err)
		return 1
	}
	fmt.Fprintf(stdout, "hpk installed into %s\n", pl.Paths.Config)
	if m.Backup != "" {
		fmt.Fprintf(stdout, "  backup: %s\n", m.Backup)
	}
	fmt.Fprintf(stdout, "  binary: %s\n  skills: %s\n  hooks:  %s\n", m.Bin, m.SkillsDir, strings.Join(m.Hooks, ", "))
	fmt.Fprintln(stdout, "Next: hpk doctor, then hpk selftest.")
	return 0
}

func profileFlag(p string) string {
	if p == "" {
		return ""
	}
	return " --profile " + p
}
