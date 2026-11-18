// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package isolate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Relay is `hpk ai-relay -- name args…`: run a Hermes program as the AI (its
// account or its container) with this process's folder, stdio and the
// variables in Forward, and return its exit code.
func Relay(args []string, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: hpk ai-relay -- hermes|hermes-acp [args…]")
		return 2
	}
	c, ok := Load()
	if !ok {
		fmt.Fprintln(stderr, "hpk ai-relay: the AI is not isolated on this machine (hpk isolate status)")
		return 2
	}
	var s Spec
	switch {
	case args[0] == "--probe" && c.Mode == ModeUser:
		// hpk isolate check: hpk itself, as the AI, reports what it can open
		s = NewSpec(c.Home, c.Bin, []string{"ai-probe", c.Private, c.Home})
	case args[0] == "--probe":
		s = NewSpec(c.Home, "hermes", []string{"--version"})
	case !Allowed(args[0]) || strings.EqualFold(strings.TrimSuffix(progBase(args[0]), ".exe"), "hpk"):
		fmt.Fprintln(stderr, "hpk ai-relay: only hermes and hermes-acp run as the AI")
		return 2
	default:
		s = NewSpec(c.Home, args[0], args[1:])
	}
	switch c.Mode {
	case ModeUser:
		code, err := relayUser(c, s)
		if err != nil {
			fmt.Fprintln(stderr, "hpk ai-relay:", err)
		}
		return code
	case ModeDocker:
		cmd := exec.Command("docker", DockerArgs(c, s)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return ee.ExitCode()
			}
			fmt.Fprintln(stderr, "hpk ai-relay: docker:", err)
			return 127
		}
		return 0
	}
	return 2
}

// ContainerHome is where the Hermes home is mounted in the container.
const ContainerHome = "/opt/data"

// ContainerPath is where a host folder is mounted in the container: the same
// path on Linux and macOS (so hpk's project records match inside), and
// /host/<drive>/<path> for a Windows path.
func ContainerPath(host string) string {
	if len(host) >= 2 && host[1] == ':' {
		rest := strings.ReplaceAll(host[2:], `\`, "/")
		return "/host/" + strings.ToLower(host[:1]) + "/" + strings.TrimLeft(rest, "/")
	}
	return filepath.ToSlash(host)
}

// toContainer maps a host path through the mounts ("" when none covers it).
func (c *Config) toContainer(p string) string {
	if p == "" {
		return ""
	}
	best, bestHost := "", ""
	for _, m := range c.Mounts {
		h := filepath.Clean(m.Host)
		if under(p, h) && len(h) > len(bestHost) {
			rel := strings.TrimLeft(filepath.ToSlash(p[len(h):]), "/")
			best, bestHost = strings.TrimRight(m.Container, "/"), h
			if rel != "" {
				best += "/" + rel
			}
		}
	}
	return best
}

func under(p, dir string) bool {
	p, dir = filepath.Clean(p), filepath.Clean(dir)
	if len(p) < len(dir) {
		return false
	}
	head := p[:len(dir)]
	same := head == dir
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		same = strings.EqualFold(head, dir)
	}
	return same && (len(p) == len(dir) || os.IsPathSeparator(p[len(dir)]))
}

// DockerArgs is the `docker exec` that runs a spec in the AI's container, as
// its hermes user, with paths mapped through the mounts.
func DockerArgs(c *Config, s Spec) []string {
	args := []string{"exec", "-i", "-u", "hermes"}
	if wd := c.toContainer(s.Dir); wd != "" {
		args = append(args, "-w", wd)
	}
	for _, kv := range s.Env {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "PATH", "TERM", "COLUMNS":
			continue // the container's own
		case "HERMES_HOME", "HPK_HOME":
			v = ContainerHome
		case "WIKI_PATH":
			if v = c.toContainer(v); v == "" {
				continue
			}
		}
		args = append(args, "-e", k+"="+v)
	}
	name := strings.TrimSuffix(progBase(s.Name), ".exe")
	return append(append(args, c.Container, name), s.Args...)
}
