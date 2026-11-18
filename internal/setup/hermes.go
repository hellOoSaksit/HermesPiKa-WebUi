// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package setup installs Hermes Agent for a person who does not have it yet,
// into hpk's own folder, only after they agree in the Console (or with
// `hpk setup hermes --yes`). It never touches a Hermes the person installed
// themselves.
//
// How: a pinned uv (checked against the SHA-256 that Hermes v0.23.1 itself pins
// in pm/lock.json), a portable Python that uv downloads (python-build-standalone,
// checked by uv), then the tested Hermes release from its GitHub tag:
//
//	<data>/hermes/uv/uv            the pinned uv
//	<data>/hermes/python/          portable Python
//	<data>/hermes/tools/           Hermes' own virtual environment
//	<data>/hermes/bin/hermes       hermes and hermes-acp (put first on PATH by hpk)
//
// Hermes Agent is MIT-licensed (Nous Research); see docs/install.md for what
// that allows and why hpk downloads instead of bundling.
package setup

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Tested versions. Change them together, after testing (docs/install.md).
const (
	HermesVersion = "v0.23.1"
	HermesSource  = "https://github.com/NousResearch/hermes-agent/archive/refs/tags/" + HermesVersion + ".tar.gz"
	HermesLicence = "MIT (Copyright (c) 2025 Nous Research)"
	PythonVersion = "3.14" // Hermes v0.23.1: requires-python >=3.11,<3.15; its own lock pins 3.14.9
	UVVersion     = "0.13.5"
)

// Artifact is one pinned download.
type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// uv per platform, copied from Hermes v0.23.1 pm/lock.json ("uv" package).
var uvArtifacts = map[string]Artifact{
	"darwin-arm64":     {"https://github.com/astral-sh/uv/releases/download/0.13.5/uv-aarch64-apple-darwin.tar.gz", "743f85035a1996ecb350659e01cd9faea9709829be09b1b4b03c73eaf3c9cd73"},
	"darwin-x64":       {"https://github.com/astral-sh/uv/releases/download/0.13.5/uv-x86_64-apple-darwin.tar.gz", "b0f52b0e3945b4107d89024ea38e625683011fbfc05a0d98f9fa2d6dcee28c66"},
	"linux-arm64":      {"https://github.com/astral-sh/uv/releases/download/0.13.5/uv-aarch64-unknown-linux-gnu.tar.gz", "4bb69cf95ed13f23713bb7bc09f241896ea0c75b7c557e615a20604b82f9c403"},
	"linux-arm64-musl": {"https://github.com/astral-sh/uv/releases/download/0.13.5/uv-aarch64-unknown-linux-musl.tar.gz", "7906c79adb21c81abd559841ba76bb1d9af9d791acac7a041b2972e03ae16538"},
	"linux-x64":        {"https://github.com/astral-sh/uv/releases/download/0.13.5/uv-x86_64-unknown-linux-gnu.tar.gz", "f68d291c75783122dad58ab77def0e13f3233f358731db5d6d7e8715603b928f"},
	"linux-x64-musl":   {"https://github.com/astral-sh/uv/releases/download/0.13.5/uv-x86_64-unknown-linux-musl.tar.gz", "57ef9d9ccb01fb1bd998df995b0dd221b52f7d7841fbea93f2d4463bf61fb7a9"},
	"win32-arm64":      {"https://github.com/astral-sh/uv/releases/download/0.13.5/uv-aarch64-pc-windows-msvc.zip", "09f47edf90726084a70f9426f6e3ca4f46e9bf7cbf23b1716ec64d60b055fae3"},
	"win32-x64":        {"https://github.com/astral-sh/uv/releases/download/0.13.5/uv-x86_64-pc-windows-msvc.zip", "a38b8d1902b7cfbc76b423ba1bb456f623f42064cdc72628b6075b89afd1019a"},
}

// Platform is the key into the pinned artifacts ("win32-x64", "linux-arm64-musl", ...).
func Platform(goos, goarch string, musl bool) string {
	os := map[string]string{"windows": "win32", "darwin": "darwin", "linux": "linux"}[goos]
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	if os == "" || arch == "" {
		return ""
	}
	k := os + "-" + arch
	if musl && goos == "linux" {
		k += "-musl"
	}
	return k
}

func isMusl() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	m, _ := filepath.Glob("/lib/ld-musl-*")
	return len(m) > 0
}

// Step is one part of the offer, shown to the person before they agree.
type Step struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	SHA256   string `json:"sha256,omitempty"`
	Download int    `json:"downloadMB"` // estimate
	Disk     int    `json:"diskMB"`     // estimate
}

// Offer is what the Console shows before asking.
type Offer struct {
	Version   string `json:"version"`
	Licence   string `json:"licence"`
	Dir       string `json:"dir"`
	Platform  string `json:"platform"`
	Supported bool   `json:"supported"`
	Steps     []Step `json:"steps"`
	Download  int    `json:"downloadMB"`
	Disk      int    `json:"diskMB"`
	Note      string `json:"note"`
}

// Dir is where hpk keeps the Hermes it installs.
func Dir(data string) string { return filepath.Join(data, "hermes") }

// BinDir holds hermes and hermes-acp after an install.
func BinDir(data string) string { return filepath.Join(Dir(data), "bin") }

// UsePath puts hpk's own Hermes first on PATH when it exists, so everything
// that looks for `hermes` and `hermes-acp` (doctor, chats, cron) finds it.
func UsePath(data string) bool {
	b := BinDir(data)
	if _, err := os.Stat(filepath.Join(b, exe("hermes"))); err != nil {
		return false
	}
	p := os.Getenv("PATH")
	for _, x := range filepath.SplitList(p) {
		if filepath.Clean(x) == filepath.Clean(b) {
			return true
		}
	}
	_ = os.Setenv("PATH", b+string(os.PathListSeparator)+p)
	return true
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// MakeOffer describes the install for this machine. Sizes are estimates.
func MakeOffer(data string) Offer {
	pf := Platform(runtime.GOOS, runtime.GOARCH, isMusl())
	uv, ok := uvArtifacts[pf]
	o := Offer{Version: HermesVersion, Licence: HermesLicence, Dir: Dir(data), Platform: pf, Supported: ok,
		Note: "Sizes are estimates. Everything goes into one folder; delete it to remove this Hermes."}
	o.Steps = []Step{
		{Name: "uv " + UVVersion + " (Python installer by Astral)", Source: uv.URL, SHA256: uv.SHA256, Download: 20, Disk: 50},
		{Name: "Python " + PythonVersion + " (portable, python-build-standalone)", Source: "https://github.com/astral-sh/python-build-standalone (chosen and checked by uv)", Download: 35, Disk: 120},
		{Name: "Hermes Agent " + HermesVersion + " with its Python packages", Source: HermesSource + " and PyPI", Download: 200, Disk: 650},
	}
	for _, s := range o.Steps {
		o.Download += s.Download
		o.Disk += s.Disk
	}
	return o
}

// ---- the install job ----

// Job is one install run; the Console polls it.
type Job struct {
	mu      sync.Mutex
	Running bool     `json:"running"`
	Step    string   `json:"step"`
	Log     []string `json:"log"`
	Error   string   `json:"error,omitempty"`
	Done    bool     `json:"done"`
	Version string   `json:"version,omitempty"` // `hermes --version` after the install
}

// Snapshot copies the job for JSON.
func (j *Job) Snapshot() Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Job{Running: j.Running, Step: j.Step, Log: append([]string(nil), j.Log...), Error: j.Error, Done: j.Done, Version: j.Version}
}

func (j *Job) say(step, line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if step != "" {
		j.Step = step
	}
	if line = strings.TrimSpace(line); line != "" {
		j.Log = append(j.Log, line)
		if len(j.Log) > 400 {
			j.Log = j.Log[len(j.Log)-400:]
		}
	}
}

// Start begins an install unless one is running. changed is called on progress.
func (j *Job) Start(data string, changed func()) error {
	j.mu.Lock()
	if j.Running {
		j.mu.Unlock()
		return errors.New("an install is already running")
	}
	j.Running, j.Done, j.Error, j.Log, j.Step = true, false, "", nil, "start"
	j.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		v, err := Install(ctx, data, func(step, line string) { j.say(step, line); changed() })
		j.mu.Lock()
		j.Running, j.Done, j.Version = false, err == nil, v
		if err != nil {
			j.Error = err.Error()
		}
		j.mu.Unlock()
		changed()
	}()
	return nil
}

// Runner runs a program; tests replace it.
var Runner = func(ctx context.Context, env []string, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// Fetch downloads a URL; tests replace it.
var Fetch = func(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 200<<20))
}

// Install runs the three steps and returns `hermes --version`.
func Install(ctx context.Context, data string, say func(step, line string)) (string, error) {
	dir := Dir(data)
	pf := Platform(runtime.GOOS, runtime.GOARCH, isMusl())
	art, ok := uvArtifacts[pf]
	if !ok {
		return "", fmt.Errorf("no tested uv for %s/%s: install Hermes Agent yourself (docs/install.md)", runtime.GOOS, runtime.GOARCH)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	uv := filepath.Join(dir, "uv", exe("uv"))
	if _, err := os.Stat(uv); err != nil {
		say("uv", "Downloading uv "+UVVersion+" from "+art.URL)
		raw, err := Fetch(ctx, art.URL)
		if err != nil {
			return "", err
		}
		if err := Verify(raw, art.SHA256); err != nil {
			return "", err
		}
		say("uv", "SHA-256 matches the pin ("+art.SHA256[:12]+"…)")
		if err := extractUV(raw, strings.HasSuffix(art.URL, ".zip"), filepath.Dir(uv)); err != nil {
			return "", err
		}
	}
	env := []string{
		"UV_PYTHON_INSTALL_DIR=" + filepath.Join(dir, "python"),
		"UV_TOOL_DIR=" + filepath.Join(dir, "tools"),
		"UV_TOOL_BIN_DIR=" + BinDir(data),
		"UV_CACHE_DIR=" + filepath.Join(dir, "cache"),
		"UV_NO_CONFIG=1",                    // ignore uv.toml / pyproject.toml around the person's folders
		"UV_PYTHON_PREFERENCE=only-managed", // never use or change a system Python
	}
	w := lineWriter(func(l string) { say("", l) })
	say("python", "Installing Python "+PythonVersion+" into "+filepath.Join(dir, "python"))
	args := []string{"python", "install", "--no-bin"}
	if runtime.GOOS == "windows" {
		args = append(args, "--no-registry") // leave the Windows Python registry alone
	}
	if err := Runner(ctx, env, w, uv, append(args, PythonVersion)...); err != nil {
		return "", fmt.Errorf("uv python install: %w", err)
	}
	say("hermes", "Installing Hermes Agent "+HermesVersion+" from "+HermesSource)
	if err := Runner(ctx, env, w, uv, "tool", "install", "--force", "--python", PythonVersion, "hermes-agent[acp] @ "+HermesSource); err != nil {
		return "", fmt.Errorf("uv tool install: %w", err)
	}
	var out bytes.Buffer
	say("check", "Checking hermes --version")
	if err := Runner(ctx, nil, &out, filepath.Join(BinDir(data), exe("hermes")), "--version"); err != nil {
		return "", fmt.Errorf("hermes --version: %w: %s", err, strings.TrimSpace(out.String()))
	}
	v := strings.TrimSpace(strings.SplitN(out.String(), "\n", 2)[0])
	say("done", "Installed: "+v)
	UsePath(data)
	return v, nil
}

// Verify checks a download against its pinned SHA-256.
func Verify(raw []byte, want string) error {
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch: got %s, want %s (download refused)", got, want)
	}
	return nil
}

// extractUV takes the uv binary (and uvx) out of the release archive.
func extractUV(raw []byte, isZip bool, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	want := map[string]bool{exe("uv"): true, exe("uvx"): true}
	found := false
	write := func(name string, r io.Reader) error {
		f, err := os.OpenFile(filepath.Join(dst, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(r, 200<<20))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if name == exe("uv") {
			found = true
		}
		return err
	}
	if isZip {
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return err
		}
		for _, f := range zr.File {
			if base := filepath.Base(f.Name); want[base] {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				err = write(base, rc)
				rc.Close()
				if err != nil {
					return err
				}
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if base := filepath.Base(h.Name); h.Typeflag == tar.TypeReg && want[base] {
				if err := write(base, tr); err != nil {
					return err
				}
			}
		}
	}
	if !found {
		return errors.New("uv archive has no uv binary")
	}
	return nil
}

type lineWriter func(string)

func (f lineWriter) Write(p []byte) (int, error) {
	for _, l := range strings.Split(strings.ReplaceAll(string(p), "\r", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			f(l)
		}
	}
	return len(p), nil
}
