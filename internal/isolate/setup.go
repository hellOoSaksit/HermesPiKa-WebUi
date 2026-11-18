// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package isolate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Step is one thing setup or teardown does. Say is printed before anything
// runs; Admin marks a change to the system (an account, a machine setting).
type Step struct {
	Say   string
	Admin bool
	Do    func() error
}

// Options for setup.
type Options struct {
	Mode      string   // user | docker
	User      string   // user: the AI's account (hpk-ai)
	Home      string   // the Hermes home
	Private   string   // the person's private folder
	Bin       string   // user: <home>/hpk/bin/hpk(.exe), the hpk the AI runs
	HermesDir string   // user: Hermes installed by hpk setup hermes ("" when on PATH elsewhere)
	ReadOnly  []string // user: program folders inside the home (Hermes' bin, its code) the AI may run but not change
	Projects  []string // project folders the AI works in
	Image     string   // docker: the image (hpk-hermes:local)
	Container string   // docker: the container name
	Compose   string   // docker: where to write the compose file

	// Recordings is the folder the person chose for browser recordings ("" = the default);
	// ResetRecordings sets it back to the default.
	Recordings      string
	ResetRecordings func() error
}

// SetupPlan lists what setup will do, and the config it will save.
func SetupPlan(o Options) ([]Step, *Config, error) {
	if o.Home == "" || o.Private == "" {
		return nil, nil, errors.New("no Hermes home or private folder")
	}
	if under(o.Private, o.Home) {
		return nil, nil, errors.New("the private folder must be outside the Hermes home, which the AI can read")
	}
	c := &Config{Mode: o.Mode, Home: o.Home, Private: o.Private, Created: time.Now().UTC()}
	var steps []Step
	switch o.Mode {
	case ModeUser:
		if o.User == "" {
			o.User = DefaultUser
		}
		if _, err := os.Stat(o.Bin); err != nil {
			return nil, nil, fmt.Errorf("hpk is not installed into Hermes yet (%s is missing); run hpk install first", o.Bin)
		}
		c.User, c.Bin = o.User, o.Bin
		s, err := userSetup(o, c)
		if err != nil {
			return nil, nil, err
		}
		steps = s
	case ModeDocker:
		if o.Container == "" {
			o.Container = DefaultUser
		}
		if o.Image == "" {
			o.Image = "hpk-hermes:local"
		}
		c.Container, c.Compose = o.Container, o.Compose
		c.Mounts = append(c.Mounts, Mount{Host: o.Home, Container: ContainerHome},
			// what the person decided stays read-only for the AI inside the shared home
			Mount{Host: TrustedDir(o.Home), Container: ContainerHome + "/hpk/trusted", ReadOnly: true})
		for _, p := range o.Projects {
			c.Mounts = append(c.Mounts, Mount{Host: p, Container: ContainerPath(p)})
		}
		steps = append(steps, Step{Say: "Make the private folder " + o.Private + " (this account only)", Do: func() error { return makePrivate(o.Private) }})
		steps = append(steps, moveSteps(o.Home, o.Private, true)...)
		steps = append(steps,
			Step{Say: "Write " + o.Compose + ": container " + o.Container + " from " + o.Image + ", mounting " + mountList(c.Mounts) + "; no ports published",
				Do: func() error { return writeCompose(o.Compose, o.Image, o.Container, c.Mounts) }},
			Step{Say: "Start it: docker compose -f " + o.Compose + " up -d", Do: func() error {
				return run("docker", "compose", "-f", o.Compose, "up", "-d")
			}})
	default:
		return nil, nil, fmt.Errorf("unknown mode %q (user or docker)", o.Mode)
	}
	if st := recordingsStep(o); st != nil {
		steps = append(steps, *st)
	}
	steps = append(steps, Step{Say: "Save these settings in " + ConfigPath() + " (this account only); hpk then starts every hermes and hermes-acp through `hpk ai-relay`",
		Do: func() error { return Save(c) }})
	return steps, c, nil
}

// TeardownPlan undoes setup: files back into the Hermes home, grants taken
// away, the account (unless keepUser) or the container removed.
func TeardownPlan(c *Config, keepUser bool) []Step {
	var steps []Step
	switch c.Mode {
	case ModeUser:
		steps = append(steps, userTeardown(c, keepUser)...)
	case ModeDocker:
		if c.Compose != "" {
			steps = append(steps, Step{Say: "Stop and remove the container: docker compose -f " + c.Compose + " down", Do: func() error {
				return run("docker", "compose", "-f", c.Compose, "down")
			}})
		}
		steps = append(steps, Step{Say: "Run `hpk install` afterwards: the container pointed Hermes' hooks at its own hpk", Do: func() error { return nil }})
	}
	steps = append(steps, moveSteps(c.Home, c.Private, false)...)
	steps = append(steps, Step{Say: "Delete " + ConfigPath() + "; hermes runs in your account again", Do: Remove})
	return steps
}

// Print writes the plan, numbered, with system changes marked.
func Print(w io.Writer, steps []Step) {
	for i, s := range steps {
		mark := ""
		if s.Admin {
			mark = "  [administrator: Windows will ask you]"
		}
		fmt.Fprintf(w, "  %d. %s%s\n", i+1, s.Say, mark)
	}
}

// Apply runs the steps in order and stops at the first failure.
func Apply(w io.Writer, steps []Step) error {
	for i, s := range steps {
		fmt.Fprintf(w, "  %d. %s … ", i+1, firstClause(s.Say))
		if err := s.Do(); err != nil {
			fmt.Fprintln(w, "failed")
			return fmt.Errorf("step %d: %w", i+1, err)
		}
		fmt.Fprintln(w, "done")
	}
	return nil
}

func firstClause(s string) string {
	if i := strings.IndexAny(s, ":;("); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func mountList(ms []Mount) string {
	var l []string
	for _, m := range ms {
		ro := ""
		if m.ReadOnly {
			ro = " (read-only)"
		}
		l = append(l, m.Host+" → "+m.Container+ro)
	}
	return strings.Join(l, ", ")
}

// moveSteps moves the person's private files between <home>/hpk and the
// private folder (toPrivate) or back. The console link is not moved: hpk
// serve writes a new one.
func moveSteps(home, private string, toPrivate bool) []Step {
	from, to := filepath.Join(home, "hpk"), private
	say := "Move the vault, browser profile and recordings and Console sessions from " + from + " to " + to
	if !toPrivate {
		from, to = to, from
		say = "Move the vault, browser profile and recordings and Console sessions back to " + to
	}
	trustFrom, trustTo := TrustedDir(home), filepath.Join(private, "trusted")
	if !toPrivate {
		trustFrom, trustTo = trustTo, trustFrom
	}
	say += "; copy what you approved (" + trustFrom + ") to " + trustTo
	return []Step{{Say: say + "; delete " + filepath.Join(home, "hpk", "console-link"), Do: func() error {
		// the copy hpk trusts: in the private folder while the AI is isolated
		// (isolate.OwnTrustedDir); back in the home when it is not
		if err := copyTrusted(trustFrom, trustTo); err != nil {
			return err
		}
		for _, rel := range Private {
			src, dst := filepath.Join(from, filepath.FromSlash(rel)), filepath.Join(to, filepath.FromSlash(rel))
			if _, err := os.Stat(src); err != nil {
				continue
			}
			if _, err := os.Stat(dst); err == nil {
				return fmt.Errorf("%s already exists; move it aside first", dst)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return err
			}
			if err := os.Rename(src, dst); err != nil {
				return err
			}
		}
		_ = os.Remove(filepath.Join(home, "hpk", "console-link"))
		return nil
	}}}
}

// Leftovers lists private files still in the Hermes home (readable by the AI).
func Leftovers(home string) []string {
	var l []string
	for _, rel := range append(Private, "console-link") {
		p := filepath.Join(home, "hpk", filepath.FromSlash(rel))
		if _, err := os.Stat(p); err == nil {
			l = append(l, p)
		}
	}
	return l
}

func writeCompose(file, image, container string, ms []Mount) error {
	var b bytes.Buffer
	b.WriteString("# Written by `hpk isolate setup --docker` (docs/isolation.md). Hermes (the AI)\n")
	b.WriteString("# runs here; hpk starts it with `docker exec`. Only these folders are shared.\n")
	fmt.Fprintf(&b, "name: %s\nservices:\n  hermes:\n    image: %s\n    container_name: %s\n", container, image, container)
	uid, gid := "10000", "10000"
	if runtime.GOOS == "linux" {
		if u, err := user.Current(); err == nil {
			uid, gid = u.Uid, u.Gid // files the AI writes in shared folders stay the person's
		}
	}
	fmt.Fprintf(&b, "    environment:\n      HERMES_UID: %q\n      HERMES_GID: %q\n", uid, gid)
	// hpk-browser in the container reaches the person's hpk serve here (Docker Desktop)
	b.WriteString("      HPK_AGENT_HOST: host.docker.internal\n")
	b.WriteString("    extra_hosts:\n      - \"host.docker.internal:host-gateway\"\n")
	b.WriteString("    volumes:\n")
	for _, m := range ms {
		v := m.Host + ":" + m.Container
		if m.ReadOnly {
			v += ":ro"
		}
		fmt.Fprintf(&b, "      - %q\n", v)
	}
	b.WriteString("    restart: unless-stopped\n")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, b.Bytes(), 0o600)
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return fmt.Errorf("%s %s: %v %s", name, strings.Join(args, " "), err, msg)
	}
	return nil
}

// ProbeResult is what `hpk ai-probe` reports, run as the AI.
type ProbeResult struct {
	User           string   `json:"user"`
	PrivateRead    []string `json:"privateRead"` // private paths the AI could open (must be empty)
	HomeWritable   bool     `json:"homeWritable"`
	HomeWriteNote  string   `json:"homeWriteNote,omitempty"`
	TrustedWritten []string `json:"trustedWritten,omitempty"` // places in <home>/hpk/trusted the AI could change (must be empty)
}

// Probe checks, from the AI's side, that the private folder is closed, the
// Hermes home is open and <home>/hpk/trusted is read-only.
func Probe(private, home string) ProbeResult {
	var r ProbeResult
	if u, err := user.Current(); err == nil {
		r.User = u.Username
	}
	try := []string{private, filepath.Join(private, "vault", "vault.bin"), filepath.Join(private, "vault", "key.dpapi"),
		filepath.Join(private, "ai-account.dpapi"), filepath.Join(private, "console-sessions.json"), filepath.Join(private, "browser", "profile")}
	for _, p := range try {
		if f, err := os.Open(p); err == nil {
			f.Close()
			r.PrivateRead = append(r.PrivateRead, p)
		}
	}
	f, err := os.CreateTemp(home, ".hpk-ai-probe-*")
	if err == nil {
		r.HomeWritable = true
		f.Close()
		os.Remove(f.Name())
	} else {
		r.HomeWriteNote = err.Error()
	}
	r.TrustedWritten = probeTrusted(TrustedDir(home))
	return r
}

// probeTrusted tries, as the AI, to add a file to dir and to open each file in
// it for writing (nothing is changed: the new file is removed, the opened ones
// are closed untouched). It returns what it could write.
func probeTrusted(dir string) []string {
	var out []string
	if f, err := os.CreateTemp(dir, ".hpk-ai-probe-*"); err == nil {
		f.Close()
		os.Remove(f.Name())
		out = append(out, dir)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if !e.Type().IsRegular() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if f, err := os.OpenFile(p, os.O_WRONLY, 0); err == nil {
			f.Close()
			out = append(out, p)
		}
	}
	return out
}

// PrintProbe writes a probe result as JSON (ai-probe's output).
func PrintProbe(w io.Writer, r ProbeResult) {
	raw, _ := json.Marshal(r)
	w.Write(append(raw, '\n'))
}

// Judge turns a probe result into problems ("" when all is well).
func Judge(c *Config, r ProbeResult) []string {
	var bad []string
	if c.Mode == ModeUser && !strings.EqualFold(lastPart(r.User), c.User) {
		bad = append(bad, fmt.Sprintf("hermes runs as %q, not %q", r.User, c.User))
	}
	for _, p := range r.PrivateRead {
		bad = append(bad, "the AI can open "+p)
	}
	if !r.HomeWritable {
		bad = append(bad, "the AI cannot write the Hermes home: "+r.HomeWriteNote)
	}
	for _, p := range r.TrustedWritten {
		bad = append(bad, "the AI can change "+p+" (it must be read-only for the AI; hpk serve or hpk isolate status repairs it)")
	}
	return bad
}

func lastPart(u string) string {
	if i := strings.LastIndexAny(u, `\/`); i >= 0 {
		return u[i+1:]
	}
	return u
}
