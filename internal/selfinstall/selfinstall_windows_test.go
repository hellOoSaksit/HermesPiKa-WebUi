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

func TestWindowsPathTwoDirs(t *testing.T) {
	a, b := `C:\d\hpk\bin`, `C:\d\hpk\hermes\bin`
	got, changed := addDirsToPath(`C:\x`, a, b)
	if !changed || got != `C:\x;`+a+`;`+b {
		t.Fatalf("got %q %v", got, changed)
	}
	got2, changed := addDirsToPath(got, a, b)
	if changed || got2 != got {
		t.Fatalf("duplicate: %q %v", got2, changed)
	}
	got3, changed := removeDirsFromPath(got, a, b)
	if !changed || got3 != `C:\x` {
		t.Fatalf("removed: %q %v", got3, changed)
	}
}
