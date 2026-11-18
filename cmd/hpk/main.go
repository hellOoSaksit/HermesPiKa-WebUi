// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Command hpk is the Hermes Project Kit: CLI, hooks and the daemon behind HPK Console.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/hook"
	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/launch"
	"hpk.local/hpk/internal/serve"
	"hpk.local/hpk/internal/setup"
	"hpk.local/hpk/internal/version"
)

const usage = `hpk — Hermes Project Kit

Usage:
  hpk [--no-open]                     start HPK Console (once per machine) and open it in the browser
  hpk stop [--addr A]                 stop hpk serve and its built-in browser
  hpk serve [--addr 127.0.0.1:7878]   run HPK Console and the daemon in this terminal
            [--tls-cert F --tls-key F]  team mode on a network (docs/team.md)
  hpk team init|add|list|role|remove|token   people and roles (team mode)
  hpk hook <name>                     answer a Hermes hook event (stdin JSON → stdout JSON)
  hpk project <command>               projects and privacy (hpk project help)
  hpk gates <command>                 modes, red lines, audit (hpk gates help)
  hpk session <command>               main chats and sub-tasks (hpk session help)
  hpk board <command>                 claims, overlaps, BOARD.md (hpk board help)
  hpk cron <command>                  scheduled jobs and budgets (hpk cron help)
  hpk acp probe                       check the ACP handshake with hermes-acp (no model call)
  hpk open                            open the Console in your browser (one-time link from hpk serve)
  hpk mcp-browser                     MCP server (stdio) giving the agent the built-in browser (needs hpk serve)
  hpk wiki <command>                  the project wiki (hpk wiki help)
  hpk install [--profile P] [--yes]   add hpk to Hermes (shows the config change first)
  hpk uninstall [--yes]               take hpk out of Hermes (projects and wikis stay)
  hpk setup hermes [--yes]            install the tested Hermes Agent into hpk's own folder
  hpk autostart on|off|status         start hpk serve when you log in (per user)
  hpk isolate setup|status|check|teardown   run Hermes (the AI) under its own OS account or container
  hpk doctor [--json]                 check everything hpk depends on
  hpk selftest                        run the core flows in a throwaway folder
  hpk version [--json]                print version information
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// A Hermes that hpk installed (hpk setup hermes) is found like one on PATH.
	setup.UsePath(launch.DataDir())
	if len(args) == 0 || strings.HasPrefix(args[0], "-") && args[0] != "-h" && args[0] != "--help" && args[0] != "-v" && args[0] != "--version" {
		return runLaunch(args, stdout, stderr)
	}
	switch args[0] {
	case "stop":
		return runStop(args[1:], stdout, stderr)
	case "autostart":
		return runAutostart(args[1:], stdout, stderr)
	case "setup":
		return runSetup(args[1:], stdout, stderr)
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		addr := fs.String("addr", "127.0.0.1:7878", "listen address; beyond loopback only in team mode (docs/team.md)")
		cert := fs.String("tls-cert", "", "TLS certificate (team mode on a network)")
		key := fs.String("tls-key", "", "TLS private key")
		plain := fs.Bool("insecure-http", false, "team mode without TLS (only behind a proxy that adds it)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		ctx, stop := signals()
		defer stop()
		if err := serve.Run(ctx, serve.Options{Addr: *addr, Console: consoleFS(), TLSCert: *cert, TLSKey: *key, InsecureHTTP: *plain}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "hook":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "hpk hook: missing hook name")
			return 2
		}
		return hook.Run(args[1], stdin, stdout)
	case "cron":
		return runCron(args[1:], stdout, stderr)
	case "team":
		return runTeam(args[1:], stdout, stderr)
	case "board":
		return runBoard(args[1:], stdout, stderr)
	case "session":
		return runSession(args[1:], stdin, stdout, stderr)
	case "open":
		return openConsole(stdout, stderr)
	case "mcp-browser":
		return mcpBrowser(stdin, stdout)
	case "acp":
		if len(args) > 1 && args[1] == "probe" {
			return runACPProbe(stdout, stderr)
		}
		fmt.Fprintln(stderr, "usage: hpk acp probe   (handshake with hermes-acp; no session, no model)")
		return 2
	case "gates":
		return runGates(args[1:], stdout, stderr)
	case "project":
		return runProject(args[1:], stdout, stderr)
	case "wiki":
		return runWiki(args[1:], stdin, stdout, stderr)
	case "isolate":
		return runIsolate(args[1:], stdin, stdout, stderr)
	case "ai-relay":
		return isolate.Relay(args[1:], stderr)
	case "ai-exec":
		return runAIExec(args[1:], stderr)
	case "ai-probe":
		return runAIProbe(args[1:], stdout)
	case "ai-account":
		return runAIAccount(args[1:], stderr)
	case "doctor":
		return runDoctor(args[1:], stdout)
	case "selftest":
		return runSelftest(stdout)
	case "install":
		return runInstall(false, args[1:], stdout, stderr)
	case "uninstall":
		return runInstall(true, args[1:], stdout, stderr)
	case "version", "--version", "-v":
		v := map[string]string{"version": version.Version, "commit": version.Commit, "date": version.Date}
		if hasFlag(args[1:], "--json") {
			return printJSON(stdout, v)
		}
		fmt.Fprintf(stdout, "hpk %s (%s, %s)\n", version.Version, version.Commit, version.Date)
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "hpk: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f {
			return true
		}
	}
	return false
}

func printJSON(w io.Writer, v any) int {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return 1
	}
	return 0
}

// consoleFS is the embedded Console, or a folder on disk when HPK_CONSOLE_DIR is
// set (front-end development against a real hpk serve).
func consoleFS() fs.FS {
	if d := os.Getenv("HPK_CONSOLE_DIR"); d != "" {
		return os.DirFS(d)
	}
	return assets.Console()
}
