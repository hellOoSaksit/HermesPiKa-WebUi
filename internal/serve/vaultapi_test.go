// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// A program on this machine (the AI's terminal) can call the API. Without the
// PIN it cannot set the first PIN over existing secrets, read, change, redirect
// or delete one; the unlock is a header token, not a cookie a browser would
// send to other localhost ports.
func TestVaultNeedsPINForAnythingThatLeaks(t *testing.T) {
	home := t.TempDir()
	s := NewServer(Options{Home: home})
	ts := httptest.NewServer(s.Routes(fstest.MapFS{"index.html": {Data: []byte("x")}}))
	defer ts.Close()
	post := func(unlock string, body map[string]any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+"/api/vault", bytes.NewReader(b))
		req.Header.Set("X-HPK", "1")
		req.Header.Set("Content-Type", "application/json")
		if unlock != "" {
			req.Header.Set("X-HPK-Vault", unlock)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		for _, c := range res.Cookies() {
			if c.Name == "hpk_vault" {
				t.Fatal("no unlock cookie")
			}
		}
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	secret := map[string]any{"op": "put", "name": "bank", "kind": "login", "user": "me", "value": "p4ssword!", "domains": []string{"bank.example"}}
	if code, _ := post("", secret); code == 200 {
		t.Fatal("no secret before a PIN exists")
	}
	code, out := post("", map[string]any{"op": "setPin", "pin": "orchid-garden-42"})
	if code != 200 || out["unlock"] == nil {
		t.Fatalf("set PIN: %d %v", code, out)
	}
	unlock := out["unlock"].(string)
	if code, out := post("", secret); code != 200 {
		t.Fatalf("adding a new secret needs no unlock: %d %v", code, out)
	}
	for _, op := range []map[string]any{
		{"op": "reveal", "name": "bank"},
		{"op": "put", "name": "bank", "kind": "login", "domains": []string{"evil.example"}},
		{"op": "delete", "name": "bank"},
		{"op": "setPin", "pin": "hijack-attempt-99"},
	} {
		if code, out := post("", op); code == 200 || out["value"] != nil {
			t.Fatalf("%v allowed without the PIN: %d %v", op["op"], code, out)
		}
		if code, _ := post("not-the-token", op); code == 200 {
			t.Fatalf("%v allowed with a made-up token", op["op"])
		}
	}
	if code, out := post(unlock, map[string]any{"op": "reveal", "name": "bank"}); code != 200 || out["value"] != "p4ssword!" {
		t.Fatalf("the person (unlocked) can see it: %d %v", code, out)
	}
	post(unlock, map[string]any{"op": "lock"})
	if code, _ := post(unlock, map[string]any{"op": "reveal", "name": "bank"}); code == 200 {
		t.Fatal("locked again")
	}
}
