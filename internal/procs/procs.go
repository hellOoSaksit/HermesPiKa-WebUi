// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package procs lists the processes of this machine (or container) and stops
// one, for the Console's background-jobs view. A process is stopped only if it
// is still the same one that was listed: same id and same start time.
package procs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Proc is one running process.
type Proc struct {
	PID   int       `json:"pid"`
	PPID  int       `json:"ppid"`
	Cmd   string    `json:"cmd"`
	Start string    `json:"start"` // opaque: start time as the OS reports it
	At    time.Time `json:"-"`     // the same start time, parsed (zero when unknown)
}

// List returns the processes this user can see.
func List(ctx context.Context) ([]Proc, error) {
	switch runtime.GOOS {
	case "linux":
		return listProc()
	case "windows":
		return listWindows(ctx)
	default:
		return listPS(ctx)
	}
}

func listProc() ([]Proc, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []Proc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		cmd := strings.TrimSpace(strings.ReplaceAll(string(raw), "\x00", " "))
		stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// fields after the ")" that closes the command name: state ppid ... starttime(22nd overall)
		s := string(stat)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(s[i+1:])
		if len(f) < 20 {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		out = append(out, Proc{PID: pid, PPID: ppid, Cmd: cmd, Start: f[19], At: bootTicks(f[19])})
	}
	return out, nil
}

func listPS(ctx context.Context) ([]Proc, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,lstart=,command=").Output()
	if err != nil {
		return nil, err
	}
	var out []Proc
	for _, l := range strings.Split(string(raw), "\n") {
		f := strings.Fields(l)
		if len(f) < 8 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		st := strings.Join(f[2:7], " ")
		at, _ := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(strings.Fields(st), " "), time.Local)
		out = append(out, Proc{PID: pid, PPID: ppid, Start: st, At: at, Cmd: strings.Join(f[7:], " ")})
	}
	return out, nil
}

func listWindows(ctx context.Context) ([]Proc, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	script := `Get-CimInstance Win32_Process | Where-Object { $_.CommandLine } | ForEach-Object { [pscustomobject]@{p=[int]$_.ProcessId;q=[int]$_.ParentProcessId;c=$_.CommandLine;s=$_.CreationDate.ToString('o')} } | ConvertTo-Json -Compress`
	raw, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return nil, err
	}
	var list []struct {
		P int    `json:"p"`
		Q int    `json:"q"`
		C string `json:"c"`
		S string `json:"s"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]Proc, 0, len(list))
	for _, x := range list {
		at, _ := time.Parse(time.RFC3339Nano, x.S)
		out = append(out, Proc{PID: x.P, PPID: x.Q, Cmd: x.C, Start: x.S, At: at})
	}
	return out, nil
}

// ErrGone: the process ended, or its id now belongs to another process.
var ErrGone = errors.New("that process is no longer running")

// Kill stops a process and its children, if it is still the one listed.
func Kill(ctx context.Context, pid int, start string) error {
	if pid <= 1 || pid == os.Getpid() {
		return errors.New("not that one")
	}
	list, err := List(ctx)
	if err != nil {
		return err
	}
	var hit *Proc
	for i := range list {
		if list[i].PID == pid {
			hit = &list[i]
		}
	}
	if hit == nil || (start != "" && hit.Start != start) {
		return ErrGone
	}
	if runtime.GOOS == "windows" {
		// the tree as listed (children first), never taskkill /T: a reused
		// parent id could pull in processes that are not this job's
		var last error
		for _, v := range append(Descendants(list, pid), pid) {
			if out, err := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(v), "/F").CombinedOutput(); err != nil && v == pid {
				last = fmt.Errorf("taskkill: %s", strings.TrimSpace(string(out)))
			}
		}
		return last
	}
	// children first, then the process; TERM, then KILL if it is still there
	victims := append(Descendants(list, pid), pid)
	for _, v := range victims {
		if p, err := os.FindProcess(v); err == nil {
			_ = p.Signal(sigterm)
		}
	}
	time.Sleep(1500 * time.Millisecond)
	for _, v := range victims {
		if alive(v) {
			if p, err := os.FindProcess(v); err == nil {
				_ = p.Kill()
			}
		}
	}
	return nil
}

// Descendants of pid, deepest first.
func Descendants(list []Proc, pid int) []int {
	kids := map[int][]int{}
	for _, p := range list {
		kids[p.PPID] = append(kids[p.PPID], p.PID)
	}
	var out []int
	var walk func(int)
	walk = func(n int) {
		for _, k := range kids[n] {
			walk(k)
			out = append(out, k)
		}
	}
	walk(pid)
	return out
}

// bootTicks turns /proc/<pid>/stat's start time (clock ticks after boot) into a time.
func bootTicks(ticks string) time.Time {
	n, err := strconv.ParseInt(ticks, 10, 64)
	if err != nil {
		return time.Time{}
	}
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(l, "btime "); ok {
			bt, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}
			}
			const hz = 100 // USER_HZ on every Linux hpk runs on
			return time.Unix(bt, 0).Add(time.Duration(n) * time.Second / hz)
		}
	}
	return time.Time{}
}

// Ancestors returns the chain of parents of pid (nearest first), stopping at loops.
func Ancestors(list []Proc, pid int) []Proc {
	by := map[int]Proc{}
	for _, p := range list {
		by[p.PID] = p
	}
	var out []Proc
	seen := map[int]bool{pid: true}
	for cur, ok := by[pid]; ok; cur, ok = by[cur.PPID] {
		if seen[cur.PPID] || cur.PPID <= 0 {
			break
		}
		seen[cur.PPID] = true
		if parent, ok := by[cur.PPID]; ok {
			out = append(out, parent)
		}
	}
	return out
}

// UnderHermes: one of pid's ancestors is a Hermes process.
func UnderHermes(list []Proc, pid int) bool {
	for _, a := range Ancestors(list, pid) {
		if IsHermes(a.Cmd) {
			return true
		}
	}
	return false
}

// IsHermes recognises the Hermes CLI, gateway and ACP server command lines.
func IsHermes(cmd string) bool {
	l := strings.ToLower(strings.ReplaceAll(cmd, "\\", "/"))
	for _, w := range []string{"/hermes ", "/hermes.exe", "hermes_cli", "hermes-acp", "hermes-agent", "/opt/hermes/"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return strings.HasPrefix(l, "hermes ") || l == "hermes"
}
