// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hpk.local/hpk/internal/browser"
	"hpk.local/hpk/internal/folders"
	"hpk.local/hpk/internal/install"
	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/launch"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/setup"
)

const isolateUsage = `hpk isolate — run Hermes (the AI) under an OS identity of its own (docs/isolation.md)

  hpk isolate setup [--dry-run] [--user hpk-ai]     a separate local account (Windows)
  hpk isolate setup --docker [--image I] [--dry-run]  a container (Docker)
  hpk isolate status                                 what is set up, quick checks
  hpk isolate check                                  start hpk as the AI and test what it can open
  hpk isolate teardown [--keep-user] [--dry-run]     undo it; Hermes runs in your account again
  hpk isolate run -- hermes …                        run a hermes command as the AI (e.g. gateway run)

setup and teardown print every step first and need you to type the confirmation
in a terminal; creating or deleting the account also shows Windows' own prompt.
Stop hpk (hpk stop) before either: they move the vault and the browser profile.
`

func runIsolate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, isolateUsage)
		return 0
	}
	switch args[0] {
	case "setup":
		return isolateSetup(args[1:], stdin, stdout, stderr)
	case "teardown":
		return isolateTeardown(args[1:], stdin, stdout, stderr)
	case "status":
		return isolateStatus(stdout)
	case "check":
		return isolateCheck(stdout, stderr)
	case "run":
		return isolate.Relay(args[1:], stderr)
	}
	fmt.Fprintf(stderr, "hpk isolate: unknown command %q\n\n%s", args[0], isolateUsage)
	return 2
}

// privateDefault is the person's private folder: inside hpk's own per-user
// folder, never inside the Hermes home.
func privateDefault() string { return filepath.Join(launch.DataDir(), "private") }

func hpkRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, r := range launch.Runs() {
		if launch.Probe(ctx, r.Addr) == launch.HPK {
			return true
		}
	}
	return false
}

// confirm asks the person to type word in a terminal. Not a pipe, not a flag:
// isolation changes who can read the vault, and a program must not say yes.
func confirm(stdin io.Reader, stdout io.Writer, word string) bool {
	if !isolate.StdinIsTerminal() {
		fmt.Fprintln(stdout, "\nThis needs a person at a terminal: run it yourself in a terminal window (not through the AI, not piped).")
		return false
	}
	fmt.Fprintf(stdout, "\nType %q to go ahead, anything else to stop: ", word)
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	return strings.TrimSpace(line) == word
}

func isolateSetup(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("isolate setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	docker := fs.Bool("docker", false, "run Hermes in a container instead of a separate account")
	user := fs.String("user", isolate.DefaultUser, "the AI's local account")
	image := fs.String("image", "hpk-hermes:local", "docker: the Hermes + hpk image (docker/hermes)")
	dry := fs.Bool("dry-run", false, "print the steps only")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	home := project.Home()
	if c, ok := isolate.Load(); ok {
		fmt.Fprintf(stderr, "hpk isolate: already set up (%s, home %s); run hpk isolate teardown first to change it\n", c.Mode, c.Home)
		return 1
	}
	if err := os.MkdirAll(folders.TrustedDir(home), 0o755); err != nil {
		fmt.Fprintln(stderr, "hpk isolate:", err)
		return 1
	}
	o := isolate.Options{Mode: isolate.ModeUser, User: *user, Home: home, Private: privateDefault(),
		Bin: install.Resolve(home, "").Bin, HermesDir: setup.Dir(launch.DataDir()), Image: *image,
		ReadOnly: []string{filepath.Join(home, "bin"), filepath.Join(home, "hermes-agent"), filepath.Join(home, "installs"), folders.TrustedDir(home)}}
	if *docker {
		o.Mode, o.Container = isolate.ModeDocker, isolate.DefaultUser
		o.Compose = filepath.Join(launch.DataDir(), "isolate", "compose.yaml")
	} else if runtime.GOOS != "windows" {
		fmt.Fprintf(stderr, "hpk isolate: a separate account is set up by hand on %s for now (docs/isolation.md), or use --docker\n", runtime.GOOS)
		return 1
	}
	list, _ := project.List(home)
	for _, p := range list {
		if _, err := os.Stat(p.Root); err == nil {
			o.Projects = append(o.Projects, p.Root)
		}
	}
	if rd := browser.RunsDir(home); rd != filepath.Join(browser.Dir(home), "runs") { // a folder the person chose
		o.Recordings, o.ResetRecordings = rd, func() error { return browser.SetRunsDir("") }
	}
	steps, _, err := isolate.SetupPlan(o)
	if err != nil {
		fmt.Fprintln(stderr, "hpk isolate:", err)
		return 1
	}
	if o.Mode == isolate.ModeUser {
		fmt.Fprintf(stdout, "hpk isolate setup: Hermes (the AI) will run as the local account %s.\n", o.User)
	} else {
		fmt.Fprintf(stdout, "hpk isolate setup: Hermes (the AI) will run in the container %s.\n", o.Container)
	}
	fmt.Fprintf(stdout, "It will then share only the Hermes home and your %d project folder(s); your vault, built-in browser and Console key stay with you.\n\n", len(o.Projects))
	isolate.Print(stdout, steps)
	if *dry {
		return 0
	}
	if hpkRunning() {
		fmt.Fprintln(stderr, "\nhpk isolate: hpk is running; stop it first (hpk stop), then run this again")
		return 1
	}
	if !confirm(stdin, stdout, "isolate") {
		fmt.Fprintln(stdout, "Nothing changed.")
		return 1
	}
	fmt.Fprintln(stdout)
	if err := isolate.Apply(stdout, steps); err != nil {
		fmt.Fprintln(stderr, "hpk isolate:", err)
		fmt.Fprintln(stderr, "The steps before it stay done; fix the cause and run hpk isolate setup again, or hpk isolate teardown.")
		return 1
	}
	return finishCheck(stdout, stderr)
}

func finishCheck(stdout, stderr io.Writer) int {
	c, ok := isolate.Load()
	if !ok {
		return 1
	}
	fmt.Fprintln(stdout, "\nChecking from the AI's side (the first start of a new account takes a while) …")
	bad, err := isolate.Check(c)
	if err != nil {
		fmt.Fprintln(stderr, "hpk isolate check:", err)
		return 1
	}
	if len(bad) > 0 {
		for _, b := range bad {
			fmt.Fprintln(stdout, "  ✗", b)
		}
		return 1
	}
	fmt.Fprintln(stdout, "  ✓ the AI cannot open your private folder, and can work in the Hermes home")
	fmt.Fprintln(stdout, "\nStart hpk again (hpk). From now on run Hermes only as the AI: hpk isolate run -- hermes …")
	fmt.Fprintln(stdout, "(e.g. the gateway: hpk isolate run -- hermes gateway run). Running hermes yourself would run")
	fmt.Fprintln(stdout, "plugins, hooks and settings the AI can change, as you. If a Hermes gateway service runs as you, move it.")
	return 0
}

func isolateTeardown(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("isolate teardown", flag.ContinueOnError)
	fs.SetOutput(stderr)
	keep := fs.Bool("keep-user", false, "keep the AI's account")
	dry := fs.Bool("dry-run", false, "print the steps only")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	c, ok := isolate.Load()
	if !ok {
		fmt.Fprintln(stdout, "hpk isolate: not set up; Hermes runs in your account")
		return 0
	}
	steps := isolate.TeardownPlan(c, *keep)
	fmt.Fprintln(stdout, "hpk isolate teardown: Hermes (the AI) will run in your own account again and could read your vault key, browser profile and Console key.")
	fmt.Fprintln(stdout)
	isolate.Print(stdout, steps)
	if *dry {
		return 0
	}
	if hpkRunning() {
		fmt.Fprintln(stderr, "\nhpk isolate: hpk is running; stop it first (hpk stop), then run this again")
		return 1
	}
	if !confirm(stdin, stdout, "teardown") {
		fmt.Fprintln(stdout, "Nothing changed.")
		return 1
	}
	fmt.Fprintln(stdout)
	if err := isolate.Apply(stdout, steps); err != nil {
		fmt.Fprintln(stderr, "hpk isolate:", err)
		return 1
	}
	return 0
}

func isolateStatus(stdout io.Writer) int {
	home := project.Home()
	if c, ok := isolate.Load(); ok {
		fmt.Fprintf(stdout, "mode      %s\n", c.Mode)
		if c.User != "" {
			fmt.Fprintf(stdout, "account   %s\n", c.User)
		}
		if c.Container != "" {
			fmt.Fprintf(stdout, "container %s (%s)\n", c.Container, c.Compose)
		}
		fmt.Fprintf(stdout, "home      %s (shared with the AI)\nprivate   %s (yours only)\nsettings  %s\nsince     %s\n\n",
			c.Home, c.Private, isolate.ConfigPath(), c.Created.Local().Format("2006-01-02 15:04"))
	}
	fail := false
	mark := map[string]string{"ok": "✓", "warn": "!", "fail": "✗", "info": "·"}
	for _, f := range isolate.Findings(home) {
		fmt.Fprintf(stdout, "%s %-14s %s\n", mark[f.Level], f.Name, f.Detail)
		if f.Fix != "" && f.Level != "ok" {
			fmt.Fprintf(stdout, "  %-14s → %s\n", "", f.Fix)
		}
		fail = fail || f.Level == "fail"
	}
	if fail {
		return 1
	}
	return 0
}

func isolateCheck(stdout, stderr io.Writer) int {
	if _, ok := isolate.Load(); !ok {
		fmt.Fprintln(stdout, "hpk isolate: not set up; Hermes runs in your account (hpk isolate setup)")
		return 1
	}
	return finishCheck(stdout, stderr)
}

// runAIExec is `hpk ai-exec <spec>`: started as the AI by the relay.
func runAIExec(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "hpk ai-exec: internal; hpk starts it")
		return 2
	}
	s, err := isolate.ReadSpecArg(args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return isolate.Exec(s)
}

// runAIProbe is `hpk ai-probe <private> <home>`, run as the AI by hpk isolate check.
func runAIProbe(args []string, stdout io.Writer) int {
	if len(args) != 2 {
		return 2
	}
	isolate.PrintProbe(stdout, isolate.Probe(args[0], args[1]))
	return 0
}

// runAIAccount is `hpk ai-account create|delete <name> [file]`, run elevated
// by setup/teardown after the person confirmed (Windows asks again).
func runAIAccount(args []string, stderr io.Writer) int {
	var err error
	switch {
	case len(args) == 3 && args[0] == "create":
		err = isolate.CreateAccount(args[1], args[2])
	case len(args) == 2 && args[0] == "delete":
		err = isolate.DeleteAccount(args[1])
	default:
		fmt.Fprintln(stderr, "hpk ai-account: internal; hpk isolate runs it")
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "hpk ai-account:", err)
		return 1
	}
	return 0
}
