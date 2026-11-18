// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package session

import (
	"errors"
	"time"

	"hpk.local/hpk/internal/project"
)

// Switch is one change of model in a chat (docs/model-switch.md). The Console
// draws it as a divider with a way back.
type Switch struct {
	At   time.Time `json:"at"`
	From string    `json:"from"` // provider:model
	To   string    `json:"to"`
	Way  string    `json:"way"`  // continue | compact
	Fill float64   `json:"fill"` // full history as a share of the new window
	By   string    `json:"by,omitempty"`
}

// RecordSwitch notes a model change and the model the chat now uses.
func RecordSwitch(p *project.Project, ref string, sw Switch) (*Session, error) {
	if sw.To == "" {
		return nil, errors.New("a switch needs the new model")
	}
	if sw.At.IsZero() {
		sw.At = time.Now().UTC()
	}
	return update(p, ref, func(s *Session) error {
		s.Model = sw.To
		s.Switches = append(s.Switches, sw)
		return nil
	})
}

// SetModel records the model a session starts on (an ask-once sub-task).
func SetModel(p *project.Project, ref, model string, askOnce bool) (*Session, error) {
	return update(p, ref, func(s *Session) error { s.Model, s.AskOnce = model, s.AskOnce || askOnce; return nil })
}
