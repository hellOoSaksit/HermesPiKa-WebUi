// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package selfinstall

import "testing"

func TestWindowsPathNoDuplicate(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\x\AppData\Local`)
	cur := `C:\a;%LOCALAPPDATA%\hpk\bin`
	got, changed := addToPath(cur, `C:\Users\x\AppData\Local\hpk\bin`)
	if changed || got != cur {
		t.Fatalf("got %q %v", got, changed)
	}
	got, changed = addToPath(`C:\a`, `C:\x y\ไทย\bin`)
	if !changed || got != `C:\a;C:\x y\ไทย\bin` {
		t.Fatalf("got %q %v", got, changed)
	}
	got, changed = addToPath("", `C:\b`)
	if !changed || got != `C:\b` {
		t.Fatalf("got %q %v", got, changed)
	}
	got, changed = removeFromPath(`C:\a;C:\B\;C:\c`, `c:\b`)
	if !changed || got != `C:\a;C:\c` {
		t.Fatalf("got %q %v", got, changed)
	}
}
