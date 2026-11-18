// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package launch

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestRunFileRoundTripAndOwnerOnlyRemove(t *testing.T) {
	t.Setenv("HPK_DATA_DIR", t.TempDir())
	r := Run{PID: 42, Addr: "127.0.0.1:7979", URL: ConsoleURL("127.0.0.1:7979"), Key: NewKey(), Version: "test"}
	done, err := WriteRun(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun("127.0.0.1:7979")
	if err != nil || got.Key != r.Key || got.PID != 42 {
		t.Fatalf("read back %+v, %v", got, err)
	}
	if list := Runs(); len(list) != 1 || list[0].Addr != r.Addr {
		t.Fatalf("Runs = %+v", list)
	}
	// A newer daemon replaced the file: the old one's cleanup must leave it.
	newer := r
	newer.Key = NewKey()
	if _, err := WriteRun(newer); err != nil {
		t.Fatal(err)
	}
	done()
	if _, err := os.Stat(RunFile(r.Addr)); err != nil {
		t.Fatal("cleanup removed another daemon's run file")
	}
	if fi, _ := os.Stat(RunFile(r.Addr)); fi != nil && fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("run file is readable by others: %v", fi.Mode())
	}
}

func TestProbe(t *testing.T) {
	ctx := context.Background()
	hpk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","version":"0.1.0"}`))
	}))
	defer hpk.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer other.Close()
	if s := Probe(ctx, strings.TrimPrefix(hpk.URL, "http://")); s != HPK {
		t.Fatalf("hpk server: %v", s)
	}
	if s := Probe(ctx, strings.TrimPrefix(other.URL, "http://")); s != Something {
		t.Fatalf("other server: %v", s)
	}
	// A free port: listen, note it, close.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	free := ln.Addr().String()
	ln.Close()
	if s := Probe(ctx, free); s != Free {
		t.Fatalf("free port: %v", s)
	}
}

func TestConsoleURLAndOpenCommand(t *testing.T) {
	if u := ConsoleURL("0.0.0.0:7878"); u != "http://127.0.0.1:7878/#/overview" {
		t.Fatal(u)
	}
	if u := ConsoleURL("127.0.0.1:9000"); u != "http://127.0.0.1:9000/#/overview" {
		t.Fatal(u)
	}
	for goos, want := range map[string]string{"windows": "rundll32", "darwin": "open", "linux": "xdg-open", "freebsd": "xdg-open"} {
		if n, args := OpenCommand(goos, "http://x/#/overview"); n != want || args[len(args)-1] != "http://x/#/overview" {
			t.Fatalf("%s: %s %v", goos, n, args)
		}
	}
}

func TestStopNeedsTheKey(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/shutdown" {
			gotKey = r.Header.Get("X-HPK-Stop-Key")
			if r.Header.Get("X-HPK") != "1" || gotKey != "right" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r) // after "stopping", health is no longer hpk
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	if err := Stop(context.Background(), Run{Addr: addr, Key: "wrong"}); err == nil {
		t.Fatal("stop with a wrong key worked")
	}
	if err := Stop(context.Background(), Run{Addr: addr, Key: "right"}); err != nil || gotKey != "right" {
		t.Fatalf("stop: %v (key %q)", err, gotKey)
	}
}

func TestAutostartDefinitions(t *testing.T) {
	base := Autostart{Exe: "/opt/my apps/hpk", Home: "/home/ann", DataDir: "/home/ann/.local/share/hpk", User: `PC\ann`, UID: 501}

	w := base
	w.GOOS, w.Exe = "windows", `C:\Users\ann\AppData\Local\hpk\bin\hpk.exe`
	path, xml := w.File()
	if !strings.HasSuffix(path, "hpk-task.xml") || !strings.Contains(xml, "<LogonTrigger>") || !strings.Contains(xml, `<UserId>PC\ann</UserId>`) ||
		!strings.Contains(xml, `--headless "C:\Users\ann\AppData\Local\hpk\bin\hpk.exe" serve`) || !strings.Contains(xml, "<RunLevel>LeastPrivilege</RunLevel>") {
		t.Fatalf("task xml:\n%s", xml)
	}
	if c := w.EnableCommands(); c[0][0] != "schtasks" || c[0][len(c[0])-1] != "/F" {
		t.Fatalf("windows enable: %v", c)
	}

	m := base
	m.GOOS = "darwin"
	path, plist := m.File()
	if path != "/home/ann/Library/LaunchAgents/local.hpk.serve.plist" && !strings.HasSuffix(path, "local.hpk.serve.plist") {
		t.Fatal(path)
	}
	if !strings.Contains(plist, "<string>/opt/my apps/hpk</string><string>serve</string>") || !strings.Contains(plist, "<key>RunAtLoad</key><true/>") {
		t.Fatalf("plist:\n%s", plist)
	}
	if c := m.EnableCommands(); c[1][0] != "launchctl" || c[1][2] != "gui/501" {
		t.Fatalf("darwin enable: %v", c)
	}

	l := base
	l.GOOS = "linux"
	path, unit := l.File()
	if !strings.HasSuffix(strings.ReplaceAll(path, `\`, "/"), ".config/systemd/user/hpk.service") && os.Getenv("XDG_CONFIG_HOME") == "" {
		t.Fatal(path)
	}
	if !strings.Contains(unit, `ExecStart="/opt/my apps/hpk" serve`) || !strings.Contains(unit, "WantedBy=default.target") {
		t.Fatalf("unit:\n%s", unit)
	}
	if c := l.EnableCommands(); strings.Join(c[1], " ") != "systemctl --user enable hpk.service" {
		t.Fatalf("linux enable: %v", c)
	}
	if (Autostart{GOOS: "plan9"}).Supported() {
		t.Fatal("plan9 should not be supported")
	}
}

func TestConsoleLink(t *testing.T) {
	home := t.TempDir()
	if _, ok := ConsoleLink(home, time.Time{}); ok {
		t.Fatal("a link without a file")
	}
	os.MkdirAll(filepath.Dir(LinkFile(home)), 0o700)
	write := func(s string) { os.WriteFile(LinkFile(home), []byte(s), 0o600) }
	write("http://127.0.0.1:7878/k/abc123\n")
	if l, ok := ConsoleLink(home, time.Time{}); !ok || l != "http://127.0.0.1:7878/k/abc123" {
		t.Fatalf("link %q %v", l, ok)
	}
	if _, ok := ConsoleLink(home, time.Now().Add(time.Hour)); ok {
		t.Fatal("a link from an earlier run was used")
	}
	for _, bad := range []string{"https://evil.example/k/x", "http://192.0.2.1:7878/k/x", "file:///etc/passwd", "http://127.0.0.1:7878/k/x\" --flag"} {
		write(bad)
		if _, ok := ConsoleLink(home, time.Time{}); ok {
			t.Fatalf("accepted %q", bad)
		}
	}
	// WaitLink sees a link that appears while it waits.
	os.Remove(LinkFile(home))
	go func() { time.Sleep(150 * time.Millisecond); write("http://127.0.0.1:7878/k/late") }()
	if l, ok := WaitLink(home, time.Now().Add(-time.Second), 2*time.Second); !ok || !strings.HasSuffix(l, "/k/late") {
		t.Fatalf("WaitLink %q %v", l, ok)
	}
}

func TestUTF16LE(t *testing.T) {
	b := utf16LE("aก😀")
	if b[0] != 0xFF || b[1] != 0xFE {
		t.Fatal("no BOM")
	}
	u := make([]uint16, 0, (len(b)-2)/2)
	for i := 2; i < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if string(utf16.Decode(u)) != "aก😀" {
		t.Fatalf("decoded %q", string(utf16.Decode(u)))
	}
}
