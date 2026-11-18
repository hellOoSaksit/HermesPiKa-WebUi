// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import "testing"

func TestRedLineTitle(t *testing.T) {
	m := redLineTitle.FindStringSubmatch("Red line (ask mode cannot pass it) — push: git push 2>&1: <terminal> (plugin approval rule)")
	if m == nil || m[1] != "push" || m[2] != "git push 2>&1" {
		t.Fatalf("%q", m)
	}
}
