// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/cron"
	"hpk.local/hpk/internal/install"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
)

const cronUsage = `hpk cron — scheduled jobs through Hermes cron (docs/DESIGN.md §12)

  hpk cron install --project P [--job nightly|weekly|digest] [--model M] [--yes]
                   without --yes: shows the scripts and hermes cron create calls; changes nothing
  hpk cron uninstall --project P [--job J] [--yes]   removes hpk's jobs from Hermes cron
  hpk cron status  --project P         jobs, pauses, locks, tokens in the last 24 h / 7 days
  hpk cron usage   --project P [--days 7] [--json]   tokens per job and run (from Hermes' own log)
  hpk cron allow   <job> --project P   un-pause a job that went over budget (a person decides)
  hpk cron set-id  <job> <hermes-job-id> --project P

Called by Hermes, not by people:
  hpk cron prep <job> --project P      0 tokens; unchanged output = Hermes skips the model
  hpk cron done <job> --project P      the job's agent calls this when it finishes
`

func runCron(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(stdout, cronUsage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	var pos []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		pos, rest = append(pos, rest[0]), rest[1:]
	}
	fs := flag.NewFlagSet("cron "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	home := fs.String("home", "", "Hermes home")
	proj := fs.String("project", os.Getenv("HPK_PROJECT"), "project name")
	job := fs.String("job", "", "only this job (install)")
	model := fs.String("model", "", "pin a cheap model for the jobs (install)")
	yes := fs.Bool("yes", false, "a person agrees to create the jobs in Hermes (install)")
	days := fs.Int("days", 7, "period (usage)")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	h := *home
	if h == "" {
		h = project.Home()
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "hpk cron "+cmd+":", err); return 1 }
	if *proj == "" {
		fmt.Fprintln(stderr, "hpk cron: give --project NAME (or set HPK_PROJECT)")
		return 2
	}
	p, err := project.Load(h, *proj)
	if err != nil {
		return fail(err)
	}
	now := time.Now()

	switch cmd {
	case "prep":
		if len(pos) == 0 {
			return 2
		}
		r, err := cron.Prep(h, p, pos[0], now)
		if err != nil {
			// Print nothing new: an error must not wake the model.
			fmt.Fprintln(stderr, "hpk cron prep:", err)
			return 0
		}
		fmt.Fprint(stdout, r.Output)
		fmt.Fprintln(stderr, "hpk cron prep:", r.Why)
		return 0

	case "done":
		if len(pos) == 0 {
			return 2
		}
		if err := cron.Done(p, pos[0], now); err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "recorded")
		return 0

	case "allow":
		if len(pos) == 0 {
			return 2
		}
		if err := cron.Allow(p, pos[0]); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s may run again\n", pos[0])
		return 0

	case "set-id":
		if len(pos) < 2 {
			return 2
		}
		if err := cron.SetHermesID(p, pos[0], pos[1]); err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "recorded")
		return 0

	case "install":
		if _, _, err := privacy.CheckActiveProfile(h, p); err != nil {
			return fail(err)
		}
		// The script calls the copy hpk install keeps in the Hermes home, so a moved or
		// rebuilt download does not break scheduled jobs; without an install, this binary.
		self, _ := os.Executable()
		if bin := install.Resolve(h, "").Bin; fileExists(bin) {
			self = bin
		}
		lang, delivery := projectLangDelivery(p)
		var plans []cron.Plan
		for _, j := range cron.Jobs(p) {
			if *job != "" && j.Name != *job {
				continue
			}
			prompt, err := cron.Prompt(p, j, assets.CronPrompts(), lang)
			if err != nil {
				return fail(err)
			}
			plans = append(plans, cron.MakePlan(h, p, j, prompt, self, delivery, *model))
		}
		if !*yes {
			if *asJSON {
				printJSON(stdout, plans)
				return exitNeedsConsent
			}
			fmt.Fprintf(stdout, "This would create %d Hermes cron jobs for %s (nothing is changed yet):\n\n", len(plans), p.Name)
			for _, pl := range plans {
				fmt.Fprintf(stdout, "• %s — deliver to %s\n  script: %s\n  hermes %s\n\n", pl.Job, pl.Deliver, pl.ScriptPath, shellQuote(pl.Args[:len(pl.Args)-1]))
			}
			fmt.Fprintln(stdout, "Each night the script runs first and costs 0 tokens; the model only runs when something changed.")
			fmt.Fprintln(stdout, "Run again with --yes to create them.")
			return exitNeedsConsent
		}
		for _, pl := range plans {
			id, err := cron.Install(h, p, pl)
			if err != nil {
				return fail(err)
			}
			if id == "" {
				fmt.Fprintf(stdout, "%s created; could not read its id — run `hermes cron list` and `hpk cron set-id %s <id> --project %s`\n", pl.Job, pl.Job, p.Name)
			} else {
				fmt.Fprintf(stdout, "%s created as %s\n", pl.Job, id)
			}
		}
		return 0

	case "uninstall":
		st, err := cron.Load(p)
		if err != nil {
			return fail(err)
		}
		var names []string
		for _, j := range cron.Jobs(p) {
			if *job == "" || j.Name == *job {
				names = append(names, j.Name)
			}
		}
		if !*yes {
			fmt.Fprintf(stdout, "This would remove these jobs of %s from Hermes cron (nothing is changed yet):\n", p.Name)
			for _, n := range names {
				id := st.Jobs[n].HermesID
				if id == "" {
					id = "not in Hermes"
				}
				fmt.Fprintf(stdout, "• %s (%s)\n", n, id)
			}
			fmt.Fprintln(stdout, "Run again with --yes to remove them.")
			return exitNeedsConsent
		}
		for _, n := range names {
			id, err := cron.Uninstall(h, p, n)
			if err != nil {
				return fail(err)
			}
			fmt.Fprintf(stdout, "%s removed%s\n", n, map[bool]string{true: " (" + id + ")", false: ""}[id != ""])
		}
		return 0

	case "status":
		if _, err := cron.Account(h, p); err != nil {
			return fail(err)
		}
		st, err := cron.Load(p)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, st)
		}
		for _, j := range cron.Jobs(p) {
			js := st.Jobs[j.Name]
			state := "not installed"
			if js != nil {
				switch {
				case js.Paused:
					state = "PAUSED: " + js.PauseReason
				case !js.LockedAt.IsZero():
					state = "running since " + js.LockedAt.Local().Format("15:04")
				case js.HermesID != "":
					state = "installed (" + js.HermesID + ")"
				default:
					state = "prepared, no Hermes id"
				}
			}
			fmt.Fprintf(stdout, "%-8s %-10s budget %6d  %s\n", j.Name, j.Schedule, j.Budget, state)
		}
		fmt.Fprintf(stdout, "tokens: %d in 24 h · %d in 7 days\n", cron.TokensSince(p, now.Add(-24*time.Hour)), cron.TokensSince(p, now.Add(-7*24*time.Hour)))
		return 0

	case "usage":
		if _, err := cron.Account(h, p); err != nil {
			return fail(err)
		}
		since := now.Add(-time.Duration(*days) * 24 * time.Hour)
		var runs []cron.Usage
		for _, u := range cron.History(p) {
			if !u.At.Before(since) {
				runs = append(runs, u)
			}
		}
		if *asJSON {
			return printJSON(stdout, runs)
		}
		type agg struct{ runs, tokens, max int }
		per := map[string]*agg{}
		for _, u := range runs {
			a := per[u.Job]
			if a == nil {
				a = &agg{}
				per[u.Job] = a
			}
			a.runs++
			a.tokens += u.Tokens
			if u.Tokens > a.max {
				a.max = u.Tokens
			}
		}
		var names []string
		for n := range per {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintf(stdout, "last %d days (model runs only; quiet nights cost 0 and are not listed)\n", *days)
		for _, n := range names {
			a := per[n]
			fmt.Fprintf(stdout, "%-8s %3d runs  %8d tokens  avg %6d  max %6d\n", n, a.runs, a.tokens, a.tokens/a.runs, a.max)
		}
		if len(names) == 0 {
			fmt.Fprintln(stdout, "no model runs recorded")
		}
		return 0

	default:
		fmt.Fprintf(stderr, "hpk cron: unknown command %q\n\n%s", cmd, cronUsage)
		return 2
	}
}

func projectLangDelivery(p *project.Project) (string, string) {
	raw, _ := os.ReadFile(p.Dir + "/project.yaml")
	var y struct {
		Language string `yaml:"language"`
		Delivery string `yaml:"delivery"`
	}
	_ = yaml.Unmarshal(raw, &y)
	lang := map[string]string{"th": "Thai", "en": "English"}[y.Language]
	return lang, y.Delivery
}

func shellQuote(args []string) string {
	var out []string
	for _, a := range args {
		if strings.ContainsAny(a, " \t\"'") {
			a = "\"" + strings.ReplaceAll(a, "\"", "\\\"") + "\""
		}
		out = append(out, a)
	}
	return strings.Join(out, " ") + " \"<prompt>\""
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
