// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package isolate

import (
	"os"
	"path/filepath"

	"hpk.local/hpk/internal/relocate"
)

// recordingsStep takes the browser recordings back into the private folder when
// the person had put them somewhere the AI can read (inside the Hermes home or
// a project folder). A folder elsewhere is left alone. nil when nothing to do.
func recordingsStep(o Options) *Step {
	rec := o.Recordings
	if rec == "" || o.ResetRecordings == nil {
		return nil
	}
	readable := under(rec, o.Home)
	for _, p := range o.Projects {
		readable = readable || under(rec, p)
	}
	if !readable {
		return nil
	}
	dst := filepath.Join(o.Private, "browser", "runs")
	return &Step{Say: "Move the browser recordings from " + rec + " (the AI can read it) to " + dst + " (private to you)", Do: func() error {
		if _, err := os.Stat(rec); err != nil {
			return o.ResetRecordings()
		}
		if err := makePrivate(dst); err != nil {
			return err
		}
		ents, _ := os.ReadDir(dst)
		if len(ents) == 0 {
			return mergeable(relocate.MoveWith(rec, dst, o.ResetRecordings))
		}
		// the default place already holds recordings (moved just before): add to them
		left, _ := os.ReadDir(rec)
		for _, e := range left {
			if err := mergeable(relocate.Move(filepath.Join(rec, e.Name()), filepath.Join(dst, e.Name()))); err != nil {
				return err
			}
		}
		return o.ResetRecordings()
	}}
}

// mergeable: a leftover .hpk-old that could not be deleted is not a failed move.
func mergeable(err error) error {
	if _, ok := err.(*relocate.CleanupError); ok {
		return nil
	}
	return err
}
