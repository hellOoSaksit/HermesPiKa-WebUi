// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hpk.local/hpk/internal/launch"
)

func quiet(t *testing.T) {
	t.Setenv("HPK_DATA_DIR", t.TempDir())
	t.Setenv("HPK_HOME", t.TempDir())
	t.Setenv("HPK_NO_BROWSER", "1")
}

func TestLaunchUsesARunningHPK(t *testing.T) {
	quiet(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","version":"9.9.9"}`))
	}))
	defer srv.Close()
	var out, errb bytes.Buffer
	if c := runLaunch([]string{"--addr", strings.TrimPrefix(srv.URL, "http://")}, &out, &errb); c != 0 || !strings.Contains(out.String(), "already running") {
		t.Fatalf("exit %d: %s %s", c, out.String(), errb.String())
	}
}

func TestLaunchRefusesAPortSomethingElseHolds(t *testing.T) {
	quiet(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	var out, errb bytes.Buffer
	if c := runLaunch([]string{"--addr", strings.TrimPrefix(srv.URL, "http://")}, &out, &errb); c != 1 || !strings.Contains(errb.String(), "another program") {
		t.Fatalf("exit %d: %s", c, errb.String())
	}
}

// One instance per port: start hpk, a second `hpk` only opens the Console,
// and `hpk stop` ends the first one.
func TestLaunchSingleInstanceAndStop(t *testing.T) {
	quiet(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	done := make(chan int, 1)
	var out1, err1 bytes.Buffer
	go func() { done <- runLaunch([]string{"--addr", addr}, &out1, &err1) }()
	if !launch.Wait(context.Background(), addr, 10*time.Second) {
		t.Fatalf("hpk did not start: %s", err1.String())
	}
	if r, err := launch.ReadRun(addr); err != nil || r.Key == "" {
		t.Fatalf("run file: %+v %v", r, err)
	}

	var out2, err2 bytes.Buffer
	if c := runLaunch([]string{"--addr", addr}, &out2, &err2); c != 0 || !strings.Contains(out2.String(), "already running") {
		t.Fatalf("second hpk: %d %s %s", c, out2.String(), err2.String())
	}

	var out3, err3 bytes.Buffer
	if c := runStop([]string{"--addr", addr}, &out3, &err3); c != 0 || !strings.Contains(out3.String(), "Stopped") {
		t.Fatalf("hpk stop: %d %s %s", c, out3.String(), err3.String())
	}
	select {
	case c := <-done:
		if c != 0 {
			t.Fatalf("first hpk exited %d: %s", c, err1.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("hpk did not stop")
	}
	if _, err := launch.ReadRun(addr); err == nil {
		t.Fatal("run file left behind")
	}
	var out4 bytes.Buffer
	runStop(nil, &out4, &out4)
	if !strings.Contains(out4.String(), "not running") {
		t.Fatalf("stop again: %s", out4.String())
	}
}
