// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/team"
)

const teamUsage = `hpk team — people and roles for HPK Console (team mode, docs/team.md)

  hpk team init --owner ID [--name N]                     start team mode; prints the owner's token once
  hpk team init --owner ID --auth header --header X-Forwarded-Email --proxy 127.0.0.1
                                                          sign-in by a reverse proxy instead of tokens
  hpk team add ID --role owner|maintainer|member|viewer [--name N] [--project P …]
  hpk team list [--json]
  hpk team role ID ROLE
  hpk team remove ID
  hpk team token ID                                       a new token; the old one stops working
  hpk team budget ID [--daily USD] [--monthly USD]        caps for what this person's chats cost (0 = none)
  hpk team off | on                                       solo mode (loopback, no sign-in) or back

Joining from a person's own hpk (docs/team.md, "Join from your own hpk"):
  hpk team join-code ID                                   a one-time invite code (15 minutes) for that member
  hpk team devices [ID] [--json]                          joined devices, last seen
  hpk team revoke-device DEVICE-ID                        that device stops working at once
  hpk team allow-ip [IP|CIDR …] [--clear]                 only these addresses may reach the server

Tokens are shown once and stored only as SHA-256 in <hermes home>/hpk/team.yaml.
`

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func runTeam(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, teamUsage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	var pos []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		pos, rest = append(pos, rest[0]), rest[1:]
	}
	fs := flag.NewFlagSet("team", flag.ContinueOnError)
	fs.SetOutput(stderr)
	home := fs.String("home", "", "Hermes home (default: auto)")
	owner := fs.String("owner", "", "the first owner's id (init)")
	name := fs.String("name", "", "display name")
	role := fs.String("role", team.Member, "role (add)")
	auth := fs.String("auth", "token", "token | header (init)")
	header := fs.String("header", "", "header the proxy sets (init, header mode)")
	asJSON := fs.Bool("json", false, "JSON output")
	daily := fs.Float64("daily", 0, "USD per day (budget)")
	monthly := fs.Float64("monthly", 0, "USD per month (budget)")
	clear := fs.Bool("clear", false, "allow-ip: remove the allowlist")
	var projects, proxies listFlag
	fs.Var(&projects, "project", "limit to a project (repeatable; add)")
	fs.Var(&proxies, "proxy", "trusted proxy IP or CIDR (repeatable; init, header mode)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	pos = append(pos, fs.Args()...)
	h := *home
	if h == "" {
		h = project.Home()
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "hpk team "+cmd+":", err); return 1 }
	c, err := team.Load(h)
	if err != nil {
		return fail(err)
	}
	need := func() bool {
		if c == nil {
			fmt.Fprintln(stderr, "hpk team "+cmd+": team mode is not set up; run hpk team init --owner <id>")
			return false
		}
		return true
	}
	save := func() int {
		if err := c.Save(h); err != nil {
			return fail(err)
		}
		return 0
	}
	showToken := func(id, token string) {
		if token == "" {
			return
		}
		fmt.Fprintf(stdout, "\nToken for %s (shown once; give it to them privately):\n  %s\n", id, token)
	}

	switch cmd {
	case "init":
		if c != nil {
			return fail(fmt.Errorf("team.yaml already exists (%s); use add, role, on or off", team.File(h)))
		}
		if *owner == "" {
			return fail(fmt.Errorf("give --owner <id>, the first owner (an email or user name)"))
		}
		c = &team.Config{Enabled: true, Auth: *auth, Header: *header, TrustedProxies: proxies}
		token, err := c.Add(*owner, *name, team.Owner, nil, time.Now())
		if err != nil {
			return fail(err)
		}
		if code := save(); code != 0 {
			return code
		}
		if wide := c.WideProxies(); len(wide) > 0 {
			fmt.Fprintf(stderr, "warning: trusted proxy %s is a range: every machine in it can claim to be anyone. Trust the proxy's own address.\n", strings.Join(wide, ", "))
		}
		fmt.Fprintf(stdout, "Team mode is on (%s sign-in). %s is the owner.\n", c.Auth, *owner)
		showToken(*owner, token)
		fmt.Fprintln(stdout, "\nRestart hpk serve. To serve other machines: hpk serve --addr 0.0.0.0:7878 --tls-cert … --tls-key …")
		return 0
	case "add":
		if !need() || len(pos) == 0 {
			return 2
		}
		token, err := c.Add(pos[0], *name, *role, projects, time.Now())
		if err != nil {
			return fail(err)
		}
		if code := save(); code != 0 {
			return code
		}
		fmt.Fprintf(stdout, "%s added as %s\n", pos[0], *role)
		showToken(pos[0], token)
		return 0
	case "list":
		if !need() {
			return 2
		}
		if *asJSON {
			type row struct {
				ID, Name, Role string
				Projects       []string
			}
			var out []row
			for _, m := range c.Members {
				out = append(out, row{m.ID, m.Label(), m.Role, m.Projects})
			}
			return printJSON(stdout, out)
		}
		fmt.Fprintf(stdout, "team mode %s · sign-in: %s\n", map[bool]string{true: "on", false: "off"}[c.Enabled], c.Auth)
		for _, m := range c.Members {
			scope := "all projects"
			if len(m.Projects) > 0 {
				scope = strings.Join(m.Projects, ", ")
			}
			fmt.Fprintf(stdout, "  %-28s %-10s %s\n", m.ID, m.Role, scope)
		}
		return 0
	case "role":
		if !need() || len(pos) < 2 {
			return 2
		}
		if err := c.SetRole(pos[0], pos[1]); err != nil {
			return fail(err)
		}
		return save()
	case "remove":
		if !need() || len(pos) == 0 {
			return 2
		}
		if err := c.Remove(pos[0]); err != nil {
			return fail(err)
		}
		if code := save(); code != 0 {
			return code
		}
		// their joined devices and pending invites go too (a running hpk serve
		// notices the file change and ends their Console sign-ins on the next request)
		d, err := team.LoadDevices(h)
		if err != nil {
			return fail(err)
		}
		if err := d.RevokeMember(pos[0]); err != nil {
			return fail(err)
		}
		return 0
	case "token":
		if !need() || len(pos) == 0 {
			return 2
		}
		token, err := c.Rotate(pos[0])
		if err != nil {
			return fail(err)
		}
		if code := save(); code != 0 {
			return code
		}
		showToken(pos[0], token)
		return 0
	case "budget":
		if !need() || len(pos) == 0 {
			return 2
		}
		if err := c.SetBudget(pos[0], *daily, *monthly); err != nil {
			return fail(err)
		}
		if code := save(); code != 0 {
			return code
		}
		fmt.Fprintf(stdout, "%s: $%.2f a day, $%.2f a month (0 = no cap)\n", pos[0], *daily, *monthly)
		return 0
	case "join-code":
		if !need() || len(pos) == 0 {
			return 2
		}
		d, err := team.LoadDevices(h)
		if err != nil {
			return fail(err)
		}
		code, exp, err := d.NewInvite(c, pos[0], "cli", time.Now())
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "Invite code for %s (one use, until %s):\n  %s\n\nThey enter it with this server's address in their own hpk: Settings > Team server.\n",
			pos[0], exp.Local().Format("15:04"), code)
		return 0
	case "devices":
		if !need() {
			return 2
		}
		d, err := team.LoadDevices(h)
		if err != nil {
			return fail(err)
		}
		who := ""
		if len(pos) > 0 {
			who = pos[0]
		}
		list := d.For(who)
		if *asJSON {
			return printJSON(stdout, list)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "no joined devices")
		}
		for _, x := range list {
			seen := "never"
			if !x.LastSeen.IsZero() {
				seen = x.LastSeen.Local().Format("2006-01-02 15:04")
			}
			fmt.Fprintf(stdout, "  %-16s %-28s %-24s last seen %s %s\n", x.ID, x.Member, x.Name, seen, x.LastIP)
		}
		return 0
	case "revoke-device":
		if !need() || len(pos) == 0 {
			return 2
		}
		d, err := team.LoadDevices(h)
		if err != nil {
			return fail(err)
		}
		if err := d.Revoke(pos[0], ""); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s revoked\n", pos[0])
		return 0
	case "allow-ip":
		if !need() {
			return 2
		}
		switch {
		case *clear:
			c.AllowIPs = nil
		case len(pos) > 0:
			c.AllowIPs = pos
		default:
			if len(c.AllowIPs) == 0 {
				fmt.Fprintln(stdout, "no allowlist: every address may reach the server (sign-in still applies)")
			} else {
				fmt.Fprintln(stdout, strings.Join(c.AllowIPs, "\n"))
			}
			return 0
		}
		if code := save(); code != 0 {
			return code
		}
		fmt.Fprintln(stdout, "saved; it applies to the next request (loopback is always allowed)")
		return 0
	case "on", "off":
		if !need() {
			return 2
		}
		c.Enabled = cmd == "on"
		if code := save(); code != 0 {
			return code
		}
		fmt.Fprintf(stdout, "team mode %s; restart hpk serve\n", cmd)
		return 0
	}
	fmt.Fprint(stderr, teamUsage)
	return 2
}
