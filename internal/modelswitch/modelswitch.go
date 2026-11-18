// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package modelswitch plans a change of model in the middle of a chat so the
// chat keeps its memory (docs/model-switch.md):
//
//   - The session brief and the wiki pages recalled into the chat always go to
//     the new model, as one carry-over block in front of the next message.
//   - Auto picks how the history travels: when the whole history fills at most
//     60% of the new model's window, the chat continues with all of it (Hermes
//     drops thinking blocks the new model cannot replay); otherwise Hermes
//     compacts it first (a summary plus the last 20 messages). Hermes keeps the
//     full history in its session store either way, so it stays searchable.
//   - Same provider needs no approval. Another provider is the "model" red line.
//     A local_only project may only switch to a local model.
package modelswitch

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// ContinueAt is the largest share of the new window the full history may fill
	// for Auto to keep all of it.
	ContinueAt = 0.60
	// TailMessages is how many recent messages a compaction keeps word for word
	// (Hermes' context compressor default, protect_last_n).
	TailMessages = 20
	// SummaryTokens is the size hpk assumes for the compaction summary.
	SummaryTokens = 2_000
	// RecallBudget caps the recalled wiki pages carried to the new model.
	RecallBudget = 4_000
	// DefaultWindow is assumed when models.dev does not know the model.
	DefaultWindow = 128_000
)

// Ways the history travels.
const (
	Continue = "continue"
	Compact  = "compact"
)

// Model is one model a chat can switch to.
type Model struct {
	ID       string  `json:"id"` // provider:model, as Hermes ACP names it
	Provider string  `json:"provider"`
	Name     string  `json:"name"`
	Window   int     `json:"window"`              // context window in tokens
	Known    bool    `json:"windowKnown"`         // the window came from models.dev
	Input    float64 `json:"inputPerM,omitempty"` // USD per million input tokens
	Write    float64 `json:"cacheWritePerM,omitempty"`
	Priced   bool    `json:"priced"`
	Local    bool    `json:"local"`
}

// ParseID splits "provider:model". An id without a provider keeps the current one.
func ParseID(id, currentProvider string) (provider, model string) {
	id = strings.TrimSpace(id)
	if i := strings.Index(id, ":"); i > 0 && !strings.Contains(id[:i], "/") {
		return strings.ToLower(id[:i]), id[i+1:]
	}
	return currentProvider, id
}

// State is the size of what a chat carries, in tokens.
type State struct {
	History int `json:"history"` // the conversation now, without brief and recall
	Tail    int `json:"tail"`    // the last TailMessages messages
	Brief   int `json:"brief"`
	Recall  int `json:"recall"`
}

// Option is what switching to one model would do.
type Option struct {
	Model         Model   `json:"model"`
	Current       bool    `json:"current,omitempty"`
	Way           string  `json:"way"`              // continue | compact
	Fill          float64 `json:"fill"`             // full history + brief + recall, as a share of the new window
	After         float64 `json:"after"`            // what is actually sent, as a share of the new window
	Carry         int     `json:"carry"`            // tokens the first turn on the new model sends
	FirstCost     float64 `json:"firstCost"`        // USD for that turn's input, nothing cached yet; 0 when unpriced
	CrossProvider bool    `json:"crossProvider"`    // the model red line: a person approves
	DropsThinking bool    `json:"dropsThinking"`    // thinking blocks of the old model are not replayed
	Locked        string  `json:"locked,omitempty"` // why it cannot be chosen
}

// Policy is what the project allows.
type Policy struct {
	LocalOnly bool
}

// Plan says what switching from cur to the model "to" would do.
func Plan(cur Model, st State, to Model, pol Policy) Option {
	win := to.Window
	if win <= 0 {
		win = DefaultWindow
	}
	full := st.History + st.Brief + st.Recall
	o := Option{Model: to, Current: to.ID == cur.ID, Fill: float64(full) / float64(win)}
	if o.Fill <= ContinueAt {
		o.Way, o.Carry = Continue, full
	} else {
		kept := SummaryTokens + st.Tail
		if kept > st.History {
			kept = st.History
		}
		o.Way, o.Carry = Compact, kept+st.Brief+st.Recall
	}
	o.After = float64(o.Carry) / float64(win)
	if to.Priced {
		// A new model has nothing cached: every token is a miss, and providers
		// that bill cache writes charge that rate on the way in.
		rate := to.Input
		if to.Write > rate {
			rate = to.Write
		}
		o.FirstCost = float64(o.Carry) * rate / 1e6
	}
	o.CrossProvider = !strings.EqualFold(cur.Provider, to.Provider)
	o.DropsThinking = !o.Current
	switch {
	case pol.LocalOnly && !to.Local:
		o.Locked = "local_only"
	case o.Current:
		o.Locked = "current"
	}
	return o
}

// Allowed checks a switch on the server: what Plan refuses stays refused, and
// another provider needs the person's confirmation.
func Allowed(o Option, confirmed bool) error {
	switch o.Locked {
	case "local_only":
		return fmt.Errorf("this project allows local models only; %s is not local", o.Model.ID)
	case "current":
		return fmt.Errorf("the chat already uses %s", o.Model.ID)
	}
	if o.CrossProvider && !confirmed {
		return fmt.Errorf("switching to another provider (%s) is a red line: confirm it", o.Model.Provider)
	}
	return nil
}

// Tokens estimates the tokens of a text: about four Latin characters per token,
// and more tokens for scripts such as Thai.
func Tokens(s string) int {
	ascii, other := 0, 0
	for _, r := range s {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+3)/4 + (other*2+2)/3
}

// Page is a wiki page recalled into the chat.
type Page struct {
	Path string
	Body string
}

// CarryText is the block in front of the first message to the new model: the
// brief and the recalled pages (within RecallBudget; pages that do not fit are
// named, not sent).
func CarryText(from, to, way, brief string, pages []Page) (string, []string) {
	var b strings.Builder
	fmt.Fprintf(&b, "[hpk] This chat moved from %s to %s. ", from, to)
	if way == Compact {
		b.WriteString("Earlier messages were compacted into a summary; the full history is still in the session store (session search).")
	} else {
		b.WriteString("The whole conversation came with it.")
	}
	b.WriteString(" Keep working from here with this context:\n\n")
	if strings.TrimSpace(brief) != "" {
		b.WriteString("## Session brief\n\n" + strings.TrimSpace(brief) + "\n\n")
	}
	used := 0
	var dropped []string
	var kept []Page
	for _, p := range pages {
		n := Tokens(p.Body)
		if used+n > RecallBudget {
			dropped = append(dropped, p.Path)
			continue
		}
		used += n
		kept = append(kept, p)
	}
	if len(kept) > 0 {
		b.WriteString("## Wiki pages recalled in this chat\n\n")
		for _, p := range kept {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", p.Path, strings.TrimSpace(p.Body))
		}
	}
	if len(dropped) > 0 {
		fmt.Fprintf(&b, "Also recalled, not included (over budget): %s. Read them from the wiki if needed.\n\n", strings.Join(dropped, ", "))
	}
	b.WriteString("---\n\n")
	return b.String(), dropped
}
