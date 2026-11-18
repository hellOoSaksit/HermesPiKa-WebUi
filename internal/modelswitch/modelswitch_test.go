// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package modelswitch

import (
	"strings"
	"testing"
)

var (
	sonnet = Model{ID: "anthropic:sonnet", Provider: "anthropic", Window: 1_000_000, Input: 3, Write: 3.75, Priced: true}
	opus   = Model{ID: "anthropic:opus", Provider: "anthropic", Window: 200_000, Input: 5, Write: 6.25, Priced: true}
	gpt    = Model{ID: "openai:gpt", Provider: "openai", Window: 400_000, Input: 1.25, Priced: true}
	qwen   = Model{ID: "ollama:qwen", Provider: "ollama", Window: 32_000, Local: true}
)

func TestContinueWhenItFits(t *testing.T) {
	o := Plan(opus, State{History: 100_000, Tail: 20_000, Brief: 1_000, Recall: 2_000}, sonnet, Policy{})
	if o.Way != Continue || o.Carry != 103_000 {
		t.Fatalf("got %s carry %d", o.Way, o.Carry)
	}
	if o.CrossProvider || o.Locked != "" || !o.DropsThinking {
		t.Fatalf("same provider switch: %+v", o)
	}
	if want := 103_000 * 3.75 / 1e6; o.FirstCost != want {
		t.Fatalf("first cost %v, want %v (cache write rate, nothing cached)", o.FirstCost, want)
	}
}

func TestCompactOverSixtyPercent(t *testing.T) {
	// 121k of a 200k window is 60.5%: compact to summary + last 20 + brief + recall.
	o := Plan(sonnet, State{History: 118_000, Tail: 15_000, Brief: 1_000, Recall: 2_000}, opus, Policy{})
	if o.Way != Compact {
		t.Fatalf("way %s, fill %.3f", o.Way, o.Fill)
	}
	if want := SummaryTokens + 15_000 + 1_000 + 2_000; o.Carry != want {
		t.Fatalf("carry %d, want %d", o.Carry, want)
	}
	if o.After >= o.Fill {
		t.Fatalf("after %.2f should be below fill %.2f", o.After, o.Fill)
	}
	// Exactly 60% still continues.
	if o := Plan(sonnet, State{History: 120_000}, opus, Policy{}); o.Way != Continue {
		t.Fatalf("60%% should continue, got %s", o.Way)
	}
}

func TestCrossProviderIsRedLine(t *testing.T) {
	o := Plan(sonnet, State{History: 10_000}, gpt, Policy{})
	if !o.CrossProvider {
		t.Fatal("anthropic → openai must be cross-provider")
	}
	if err := Allowed(o, false); err == nil {
		t.Fatal("unconfirmed cross-provider switch must be refused")
	}
	if err := Allowed(o, true); err != nil {
		t.Fatal(err)
	}
	if err := Allowed(Plan(sonnet, State{}, opus, Policy{}), false); err != nil {
		t.Fatalf("same provider needs no confirmation: %v", err)
	}
}

func TestLocalOnlyLocked(t *testing.T) {
	o := Plan(qwen, State{History: 1_000}, sonnet, Policy{LocalOnly: true})
	if o.Locked != "local_only" || Allowed(o, true) == nil {
		t.Fatalf("remote model on local_only must stay locked even when confirmed: %+v", o)
	}
	other := Model{ID: "ollama:llama", Provider: "ollama", Window: 8_000, Local: true}
	if o := Plan(qwen, State{History: 6_000, Tail: 3_000}, other, Policy{LocalOnly: true}); o.Locked != "" || o.Way != Compact {
		t.Fatalf("local → local allowed, compacted: %+v", o)
	}
	if o := Plan(sonnet, State{}, sonnet, Policy{}); o.Locked != "current" || o.DropsThinking {
		t.Fatalf("current model: %+v", o)
	}
}

func TestUnknownWindowAndPrice(t *testing.T) {
	o := Plan(sonnet, State{History: 90_000}, Model{ID: "anthropic:new", Provider: "anthropic"}, Policy{})
	if o.Way != Compact || o.FirstCost != 0 {
		t.Fatalf("unknown window uses %d: %+v", DefaultWindow, o)
	}
	// A summary never exceeds a short history.
	if o := Plan(sonnet, State{History: 500, Tail: 400}, Model{ID: "x:y", Provider: "x", Window: 600}, Policy{}); o.Carry != 500 {
		t.Fatalf("carry %d", o.Carry)
	}
}

func TestParseID(t *testing.T) {
	cases := map[string][2]string{
		"anthropic:claude-opus-4": {"anthropic", "claude-opus-4"},
		"claude-opus-4":           {"cur", "claude-opus-4"},
		"openrouter/x:free":       {"cur", "openrouter/x:free"},
	}
	for in, want := range cases {
		if p, m := ParseID(in, "cur"); p != want[0] || m != want[1] {
			t.Errorf("%s → %s %s", in, p, m)
		}
	}
}

func TestCarryText(t *testing.T) {
	big := strings.Repeat("word ", 20_000)
	txt, dropped := CarryText("sonnet", "opus", Compact, "# 1 — Release\n\nShip it.", []Page{{"decisions/a.md", "Use JWT."}, {"big.md", big}})
	for _, want := range []string{"Session brief", "Ship it.", "decisions/a.md", "Use JWT.", "session search", "big.md"} {
		if !strings.Contains(txt, want) {
			t.Errorf("carry text lacks %q", want)
		}
	}
	if strings.Contains(txt, big[:200]) || len(dropped) != 1 {
		t.Fatalf("over-budget page must be named, not sent (dropped %v)", dropped)
	}
	if Tokens("abcd") != 1 || Tokens("สวัสดี") < 4 {
		t.Fatalf("tokens: %d %d", Tokens("abcd"), Tokens("สวัสดี"))
	}
}
