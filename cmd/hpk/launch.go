// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"hpk.local/hpk/internal/launch"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/serve"
	"hpk.local/hpk/internal/setup"
)

// signals ends serve on Ctrl+C, and on SIGTERM where the OS sends it
// (systemd, launchd, docker stop).
func signals() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// runLaunch is `hpk` with no arguments: one program to start everything.
// If hpk already runs on the address, it only opens the Console; otherwise it
// runs the daemon here (Ctrl+C or `hpk stop` ends it, with its built-in
// browser) and opens the Console once it listens.
func runLaunch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hpk", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", launch.DefaultAddr, "listen address (this machine only)")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	fs.Bool("no-install", false, "do not install hpk for you (accepted for scripts; only a bare `hpk` self-installs)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, stop := signals()
	defer stop()
	url := launch.ConsoleURL(*addr)
	open := func(since time.Time, wait time.Duration) {
		if !*noOpen {
			openConsoleLink(url, since, wait, stderr)
		}
	}
	switch launch.Probe(ctx, *addr) {
	case launch.HPK:
		fmt.Fprintf(stdout, "hpk is already running: %s\n", url)
		open(time.Time{}, 0)
		return 0
	case launch.Something:
		fmt.Fprintf(stderr, "hpk: another program uses %s. Stop it, or run: hpk --addr 127.0.0.1:<port>\n", *addr)
		return 1
	}
	fmt.Fprintf(stdout, "Starting HPK Console on %s\nStop with Ctrl+C or `hpk stop`.\n", url)
	started := time.Now().Add(-2 * time.Second) // file times can be coarse
	err := serve.Run(ctx, serve.Options{Addr: *addr, Console: consoleFS(), Ready: func(string) { open(started, 3*time.Second) }})
	if err != nil && isAddrInUse(err) {
		// Another `hpk` started at the same moment and holds the port (the lock): use it.
		if launch.Wait(context.Background(), *addr, 10*time.Second) {
			fmt.Fprintf(stdout, "hpk is already running: %s\n", url)
			open(time.Time{}, 0)
			return 0
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "hpk stopped.")
	return 0
}

// openConsoleLink opens the one-time Console link that hpk serve writes to
// <private>/console-link (internal/isolate) (this browser then gets the Console key), and
// the bare URL only when there is no link. A link written before since is from
// an earlier run and ignored; wait is how long to wait for serve to write it.
// `hpk open` can share this with since = time.Time{} and wait = 0.
func openConsoleLink(url string, since time.Time, wait time.Duration, stderr io.Writer) {
	target := url
	if link, ok := launch.WaitLink(project.Home(), since, wait); ok {
		target = link
	}
	if err := launch.Open(target); err != nil {
		fmt.Fprintf(stderr, "Could not open a browser (%v). Open this yourself (a /k/ link works once, for 10 minutes):\n  %s\n", err, target)
	}
}

func isAddrInUse(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "address already in use") || strings.Contains(s, "only one usage of each socket address")
}

// runStop is `hpk stop`: end the daemon(s) this person started.
func runStop(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "", "stop only the hpk on this address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	runs := launch.Runs()
	if *addr != "" {
		r, err := launch.ReadRun(*addr)
		runs = nil
		if err == nil {
			runs = []launch.Run{r}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stopped := 0
	for _, r := range runs {
		if launch.Probe(ctx, r.Addr) != launch.HPK {
			_ = os.Remove(launch.RunFile(r.Addr)) // left behind by a crash
			continue
		}
		if err := launch.Stop(ctx, r); err != nil {
			fmt.Fprintln(stderr, "hpk stop:", err)
			return 1
		}
		fmt.Fprintf(stdout, "Stopped hpk on %s\n", r.Addr)
		stopped++
	}
	if stopped == 0 {
		fmt.Fprintln(stdout, "hpk is not running.")
	}
	return 0
}

// runAutostart is `hpk autostart on|off|status`: start hpk serve at login.
func runAutostart(args []string, stdout, stderr io.Writer) int {
	a, err := launch.ThisAutostart()
	if err != nil {
		fmt.Fprintln(stderr, "hpk autostart:", err)
		return 1
	}
	cmd := "status"
	if len(args) > 0 {
		cmd = args[0]
	}
	file, _ := a.File()
	switch cmd {
	case "on":
		err = a.Enable()
	case "off":
		err = a.Disable()
	case "status":
	default:
		fmt.Fprintln(stderr, "usage: hpk autostart on|off|status")
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "hpk autostart:", err)
		return 1
	}
	if a.Enabled() {
		fmt.Fprintf(stdout, "Start at login: on (%s runs `hpk serve`; %s)\n", a.Exe, file)
	} else {
		fmt.Fprintln(stdout, "Start at login: off")
	}
	return 0
}

// runSetup is `hpk setup hermes [--yes]`: the Console's first-run install, from a terminal.
func runSetup(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "hermes" {
		fmt.Fprintln(stderr, "usage: hpk setup hermes [--yes]   install the tested Hermes Agent into hpk's own folder")
		return 2
	}
	data := launch.DataDir()
	o := setup.MakeOffer(data)
	if !hasFlag(args[1:], "--yes") {
		fmt.Fprintf(stdout, "hpk setup hermes would install Hermes Agent %s (licence: %s) into\n  %s\n\n", o.Version, o.Licence, o.Dir)
		for _, s := range o.Steps {
			fmt.Fprintf(stdout, "  • %s\n    from %s\n    about %d MB download, %d MB on disk\n", s.Name, s.Source, s.Download, s.Disk)
			if s.SHA256 != "" {
				fmt.Fprintf(stdout, "    SHA-256 %s\n", s.SHA256)
			}
		}
		fmt.Fprintf(stdout, "\nTotal: about %d MB download, %d MB on disk. %s\n", o.Download, o.Disk, o.Note)
		if !o.Supported {
			fmt.Fprintf(stdout, "\nThis platform (%s) has no tested uv: install Hermes yourself (docs/install.md).\n", o.Platform)
			return 1
		}
		fmt.Fprintln(stdout, "\nTo go ahead: hpk setup hermes --yes")
		return exitNeedsConsent
	}
	ctx, stop := signals()
	defer stop()
	v, err := setup.Install(ctx, data, func(step, line string) {
		if line != "" {
			fmt.Fprintln(stdout, line)
		}
	})
	if err != nil {
		fmt.Fprintln(stderr, "hpk setup hermes:", err)
		return 1
	}
	fmt.Fprintf(stdout, "\n%s is ready. hpk finds it by itself; to use `hermes` in a terminal, add %s to PATH.\nNext: hpk install, then `hermes setup` to connect a model account.\n", v, setup.BinDir(data))
	return 0
}
