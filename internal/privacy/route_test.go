// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package privacy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hpk.local/hpk/internal/project"
)

func TestIsLocal(t *testing.T) {
	cases := []struct {
		r     Route
		extra []string
		want  bool
	}{
		{Route{BaseURL: "http://127.0.0.1:11434/v1"}, nil, true},
		{Route{BaseURL: "http://localhost:1234/v1"}, nil, true},
		{Route{BaseURL: "http://[::1]:8080/v1"}, nil, true},
		{Route{BaseURL: "http://gpu.localhost/v1"}, nil, true},
		{Route{Provider: "lmstudio"}, nil, true},
		{Route{Provider: "anthropic"}, nil, false},
		{Route{Provider: "auto"}, nil, false},
		{Route{Provider: "lmstudio", BaseURL: "https://lm.example.com/v1"}, nil, false},
		{Route{BaseURL: "https://openrouter.ai/api/v1"}, nil, false},
		{Route{BaseURL: "http://192.168.1.20:8000/v1"}, nil, false},
		{Route{BaseURL: "http://192.168.1.20:8000/v1"}, []string{"192.168.1.20"}, true},
		{Route{BaseURL: "http://localhost.example.com/v1"}, nil, false},
		{Route{BaseURL: "::not a url"}, nil, false},
	}
	for _, c := range cases {
		if got := IsLocal(c.r, c.extra); got != c.want {
			t.Errorf("IsLocal(%+v, %v) = %v, want %v", c.r, c.extra, got, c.want)
		}
	}
}

func TestLocalOnlyNeedsALocalRoute(t *testing.T) {
	home, p := setup(t, "private")
	if v, _ := CheckModel(home, p, "s1"); v != Allow {
		t.Fatal("model_policy any must not check routes")
	}
	p.Privacy.ModelPolicy = LocalOnly
	if v, why := CheckModel(home, p, "s1"); v != Block || !strings.Contains(why, "fail closed") {
		t.Fatalf("no route seen must block: %v %s", v, why)
	}
	RecordRoute(home, "s1", Route{Provider: "custom", BaseURL: "http://127.0.0.1:11434/v1", Model: "qwen"})
	if v, why := CheckModel(home, p, "s1"); v != Allow {
		t.Fatalf("local route: %s", why)
	}
	// An auxiliary call (title, compression) to a remote host taints the session for good.
	RecordRoute(home, "s1", Route{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1"})
	RecordRoute(home, "s1", Route{Provider: "custom", BaseURL: "http://127.0.0.1:11434/v1", Model: "qwen"})
	if v, why := CheckModel(home, p, "s1"); v != Block || !strings.Contains(why, "openrouter.ai") {
		t.Fatalf("a session that sent content out must stay blocked: %v %s", v, why)
	}
	// Another session is judged on its own.
	RecordRoute(home, "s2", Route{BaseURL: "http://localhost:1234/v1"})
	if v, _ := CheckModel(home, p, "s2"); v != Allow {
		t.Fatal("s2 is local")
	}
	// A host the project lists counts as this machine.
	RecordRoute(home, "s3", Route{BaseURL: "http://192.168.1.20:8000/v1"})
	if v, _ := CheckModel(home, p, "s3"); v != Block {
		t.Fatal("a LAN host is remote until the project lists it")
	}
	p.Privacy.LocalHosts = []string{"192.168.1.20"}
	if v, why := CheckModel(home, p, "s3"); v != Allow {
		t.Fatalf("listed host: %s", why)
	}
}

func TestActiveProfileRoute(t *testing.T) {
	home, p := setup(t, "private")
	p.Privacy.ModelPolicy = LocalOnly
	t.Setenv("HERMES_HOME", "")
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("model:\n  default: big\n  provider: auto\n  base_url: https://openrouter.ai/api/v1\n"), 0o644)
	if _, _, err := CheckActiveProfile(home, p); err == nil || !strings.Contains(err.Error(), "openrouter.ai") {
		t.Fatalf("remote default profile must be refused: %v", err)
	}
	os.MkdirAll(filepath.Join(home, "profiles", "offline"), 0o755)
	os.WriteFile(filepath.Join(home, "profiles", "offline", "config.yaml"), []byte("model:\n  default: qwen\n  provider: custom\n  base_url: http://127.0.0.1:11434/v1\n"), 0o644)
	os.WriteFile(filepath.Join(home, "active_profile"), []byte("offline\n"), 0o644)
	if prof, _, err := CheckActiveProfile(home, p); err != nil || prof != "offline" {
		t.Fatalf("local active profile: %s %v", prof, err)
	}
}

func TestCostAndMoneyCaps(t *testing.T) {
	home, p := setup(t, "private")
	cache := `{"anthropic":{"models":{"m-big":{"cost":{"input":2,"output":10,"cache_read":0.2,"cache_write":2.5}}}},"other":{"models":{"free":{}}}}`
	os.WriteFile(filepath.Join(home, "models_dev_cache.json"), []byte(cache), 0o644)
	if pr, ok := PriceOf(home, "openrouter", "anthropic/m-big"); !ok || pr.Output != 10 {
		t.Fatalf("price by model id behind a vendor prefix: %+v %v", pr, ok)
	}
	project.LinkSession(p, "s-money")
	r := Route{Provider: "anthropic", BaseURL: "https://api.anthropic.com", Model: "m-big"}
	// 1M input ($2) + 100k output ($1) + 1M cache read ($0.20) = $3.20
	RecordUsage(home, "s-money", r, map[string]any{"input_tokens": 1e6, "output_tokens": 1e5, "cache_read_tokens": 1e6})
	if got := Spent(home, p, time.Now().Add(-time.Hour)); got < 3.19 || got > 3.21 {
		t.Fatalf("spent %.4f, want 3.20", got)
	}
	if over, _ := OverMoney(home, p, time.Now()); over {
		t.Fatal("no cap set: never over")
	}
	project.SetField(home, p.Name, []string{"budget", "money_daily_usd"}, 3.0, "test")
	p2, _ := project.Load(home, p.Name)
	if over, why := OverMoney(home, p2, time.Now()); !over || !strings.Contains(why, "$3.20") {
		t.Fatalf("over the daily cap: %v %s", over, why)
	}
	if raw, err := os.ReadFile(filepath.Join(home, "hpk", "prices.json")); err != nil || len(raw) > 2000 {
		t.Fatal("prices are extracted into a small file")
	}
}

func TestWindowOf(t *testing.T) {
	home := t.TempDir()
	cache := `{"anthropic":{"models":{"m-big":{"cost":{"input":2,"output":10},"limit":{"context":200000}}}}}`
	os.WriteFile(filepath.Join(home, "models_dev_cache.json"), []byte(cache), 0o644)
	// An older prices.json without windows is rebuilt.
	os.MkdirAll(filepath.Join(home, "hpk"), 0o755)
	if n, ok := WindowOf(home, "anthropic", "m-big"); !ok || n != 200000 {
		t.Fatalf("window %d %v", n, ok)
	}
	if n, ok := WindowOf(home, "openrouter", "anthropic/m-big"); !ok || n != 200000 {
		t.Fatalf("window behind a vendor prefix %d %v", n, ok)
	}
	if _, ok := WindowOf(home, "x", "nope"); ok {
		t.Fatal("unknown model has no window")
	}
}
