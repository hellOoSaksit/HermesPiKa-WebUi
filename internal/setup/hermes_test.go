// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPlatformAndPins(t *testing.T) {
	cases := map[[3]string]string{
		{"windows", "amd64", ""}: "win32-x64", {"darwin", "arm64", ""}: "darwin-arm64",
		{"linux", "amd64", ""}: "linux-x64", {"linux", "arm64", "musl"}: "linux-arm64-musl", {"plan9", "amd64", ""}: "",
	}
	for in, want := range cases {
		if got := Platform(in[0], in[1], in[2] == "musl"); got != want {
			t.Fatalf("%v: %q, want %q", in, got, want)
		}
	}
	for k, a := range uvArtifacts {
		if !strings.Contains(a.URL, "/"+UVVersion+"/") || len(a.SHA256) != 64 {
			t.Fatalf("%s: bad pin %+v", k, a)
		}
	}
	o := MakeOffer(t.TempDir())
	if o.Version != "v0.23.1" || len(o.Steps) != 3 || o.Download <= 0 || !strings.Contains(o.Licence, "MIT") {
		t.Fatalf("offer %+v", o)
	}
}

func TestVerify(t *testing.T) {
	raw := []byte("uv")
	sum := sha256.Sum256(raw)
	if err := Verify(raw, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if err := Verify([]byte("tampered"), hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("a wrong checksum was accepted")
	}
}

func TestExtractUV(t *testing.T) {
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for _, n := range []string{exe("uv"), exe("uvx"), "README"} {
		w, _ := zw.Create(n)
		w.Write([]byte("bin:" + n))
	}
	zw.Close()
	dst := t.TempDir()
	if err := extractUV(zb.Bytes(), true, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, exe("uv"))); string(b) != "bin:"+exe("uv") {
		t.Fatalf("zip uv = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dst, "README")); err == nil {
		t.Fatal("extracted more than uv")
	}

	var tb bytes.Buffer
	gz := gzip.NewWriter(&tb)
	tw := tar.NewWriter(gz)
	body := []byte("tar-uv")
	tw.WriteHeader(&tar.Header{Name: "uv-x86_64-unknown-linux-gnu/" + exe("uv"), Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	dst2 := t.TempDir()
	if err := extractUV(tb.Bytes(), false, dst2); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst2, exe("uv"))); string(b) != "tar-uv" {
		t.Fatalf("tar uv = %q", b)
	}
	if err := extractUV(zipOf("nothing"), true, t.TempDir()); err == nil {
		t.Fatal("an archive without uv was accepted")
	}
}

func zipOf(name string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, _ := w.Create(name)
	f.Write([]byte("x"))
	w.Close()
	return b.Bytes()
}

// The whole flow with fake downloads and programs: pinned uv, checksum,
// python install, tool install of the pinned tag, version check, PATH.
func TestInstallFlow(t *testing.T) {
	pf := Platform(runtime.GOOS, runtime.GOARCH, isMusl())
	art, ok := uvArtifacts[pf]
	if !ok {
		t.Skip("no pinned uv for this platform")
	}
	var archive []byte
	if strings.HasSuffix(art.URL, ".zip") {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		f, _ := w.Create(exe("uv"))
		f.Write([]byte("fake uv"))
		w.Close()
		archive = b.Bytes()
	} else {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(&tar.Header{Name: "uv/" + exe("uv"), Mode: 0o755, Size: 7, Typeflag: tar.TypeReg})
		tw.Write([]byte("fake uv"))
		tw.Close()
		gz.Close()
		archive = b.Bytes()
	}
	sum := sha256.Sum256(archive)
	saved := uvArtifacts[pf]
	uvArtifacts[pf] = Artifact{URL: art.URL, SHA256: hex.EncodeToString(sum[:])}
	defer func() { uvArtifacts[pf] = saved }()

	oldFetch, oldRun := Fetch, Runner
	defer func() { Fetch, Runner = oldFetch, oldRun }()
	Fetch = func(ctx context.Context, url string) ([]byte, error) {
		if url != art.URL {
			return nil, errors.New("unexpected download " + url)
		}
		return archive, nil
	}
	data := t.TempDir()
	var calls []string
	Runner = func(ctx context.Context, env []string, out io.Writer, name string, args ...string) error {
		calls = append(calls, filepath.Base(name)+" "+strings.Join(args, " "))
		if len(args) > 1 && args[0] == "tool" {
			if !strings.Contains(strings.Join(env, " "), "UV_TOOL_BIN_DIR="+BinDir(data)) {
				t.Errorf("tool install without hpk's bin dir: %v", env)
			}
			os.MkdirAll(BinDir(data), 0o755)
			os.WriteFile(filepath.Join(BinDir(data), exe("hermes")), []byte("x"), 0o755)
		}
		if len(args) == 1 && args[0] == "--version" {
			out.Write([]byte("Hermes Agent v0.23.1\n"))
		}
		return nil
	}
	t.Setenv("PATH", os.Getenv("PATH"))
	v, err := Install(context.Background(), data, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if v != "Hermes Agent v0.23.1" {
		t.Fatalf("version %q", v)
	}
	all := strings.Join(calls, "\n")
	if !strings.Contains(all, "python install --no-bin") || !strings.Contains(all, "hermes-agent[acp] @ "+HermesSource) || !strings.Contains(HermesSource, "/v0.23.1.tar.gz") {
		t.Fatalf("calls:\n%s", all)
	}
	if !strings.HasPrefix(os.Getenv("PATH"), BinDir(data)) {
		t.Fatal("hpk's Hermes is not first on PATH")
	}
}
