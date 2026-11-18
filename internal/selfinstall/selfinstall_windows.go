// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package selfinstall

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// expand resolves %VAR% references the way Windows does.
func expand(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '%' {
			if j := strings.IndexByte(p[i+1:], '%'); j > 0 {
				if v, ok := os.LookupEnv(p[i+1 : i+1+j]); ok {
					b.WriteString(v)
					i += j + 1
					continue
				}
			}
		}
		b.WriteByte(p[i])
	}
	return b.String()
}

func key(p string) string {
	return strings.ToLower(strings.TrimRight(filepath.Clean(expand(strings.TrimSpace(p))), `\/`))
}

// addToPath appends dir to the PATH value cur unless an entry already names
// it (case-insensitive, after expanding %VAR%). Pure: no registry.
func addToPath(cur, dir string) (string, bool) {
	for _, e := range strings.Split(cur, ";") {
		if e != "" && key(e) == key(dir) {
			return cur, false
		}
	}
	if strings.TrimSpace(cur) == "" {
		return dir, true
	}
	return strings.TrimRight(cur, ";") + ";" + dir, true
}

func removeFromPath(cur, dir string) (string, bool) {
	var keep []string
	changed := false
	for _, e := range strings.Split(cur, ";") {
		if e != "" && key(e) == key(dir) {
			changed = true
			continue
		}
		keep = append(keep, e)
	}
	return strings.Join(keep, ";"), changed
}

// addDirsToPath adds each dir in turn (no duplicates). Pure.
func addDirsToPath(cur string, dirs ...string) (string, bool) {
	changed := false
	for _, d := range dirs {
		var c bool
		if cur, c = addToPath(cur, d); c {
			changed = true
		}
	}
	return cur, changed
}

func removeDirsFromPath(cur string, dirs ...string) (string, bool) {
	changed := false
	for _, d := range dirs {
		var c bool
		if cur, c = removeFromPath(cur, d); c {
			changed = true
		}
	}
	return cur, changed
}

func editPath(f func(cur string) (string, bool)) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	cur, typ, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return false, err
	}
	if err == registry.ErrNotExist {
		typ = registry.EXPAND_SZ
	}
	next, changed := f(cur)
	if !changed {
		return false, nil
	}
	if typ == registry.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", next)
	} else {
		err = k.SetStringValue("Path", next)
	}
	if err != nil {
		return false, err
	}
	broadcastEnvChange()
	return true, nil
}

func addPath(dirs ...string) (bool, error) {
	return editPath(func(cur string) (string, bool) { return addDirsToPath(cur, dirs...) })
}

func removePath(dirs ...string) (bool, error) {
	return editPath(func(cur string) (string, bool) { return removeDirsFromPath(cur, dirs...) })
}

// broadcastEnvChange tells open programs (Explorer, new terminals) that the
// environment changed, so they re-read PATH.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	env, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc := syscall.NewLazyDLL("user32.dll").NewProc("SendMessageTimeoutW")
	var res uintptr
	_, _, _ = proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&res)))
}

func shortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "HPK Console.lnk")
}

func psq(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func addShortcut(target string) (bool, error) {
	if os.Getenv("APPDATA") == "" {
		return false, fmt.Errorf("APPDATA is not set")
	}
	lnk := shortcutPath()
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return false, err
	}
	script := "$s=(New-Object -ComObject WScript.Shell).CreateShortcut(" + psq(lnk) + ");" +
		"$s.TargetPath=" + psq(target) + ";$s.WorkingDirectory=" + psq(filepath.Dir(target)) + ";" +
		"$s.Description='HPK Console';$s.Save()"
	if out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput(); err != nil {
		return false, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

func removeShortcut() error {
	if err := os.Remove(shortcutPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
