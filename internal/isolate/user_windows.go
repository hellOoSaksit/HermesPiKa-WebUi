// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package isolate

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	advapi32                    = windows.NewLazySystemDLL("advapi32.dll")
	procCreateProcessWithLogonW = advapi32.NewProc("CreateProcessWithLogonW")
	netapi32                    = windows.NewLazySystemDLL("netapi32.dll")
	procNetUserAdd              = netapi32.NewProc("NetUserAdd")
	procNetUserDel              = netapi32.NewProc("NetUserDel")
	procNetUserSetInfo          = netapi32.NewProc("NetUserSetInfo")
)

const (
	logonWithProfile = 0x1
	nerrUserExists   = 2224
	nerrUserNotFound = 2221
)

// PasswordFile keeps the AI account's password for the person only: DPAPI
// (the person's account) inside the private folder.
func PasswordFile(c *Config) string { return filepath.Join(c.Private, "ai-account.dpapi") }

// relayUser starts `<bin> ai-exec <spec>` as the AI's account with this
// process's stdin/stdout/stderr, in a job that ends the whole tree when this
// relay ends, and waits for it.
func relayUser(c *Config, s Spec) (int, error) {
	pw, err := readPassword(PasswordFile(c))
	if err != nil {
		return 127, fmt.Errorf("the AI account's password: %w (run hpk isolate setup again)", err)
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 127, err
	}
	defer windows.CloseHandle(job)
	var lim windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	lim.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&lim)), uint32(unsafe.Sizeof(lim))); err != nil {
		return 127, err
	}

	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES | windows.STARTF_USESHOWWINDOW
	si.ShowWindow = windows.SW_HIDE
	si.StdInput, _ = windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	si.StdOutput, _ = windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	si.StdErr, _ = windows.GetStdHandle(windows.STD_ERROR_HANDLE)

	line := windows.EscapeArg(c.Bin) + " ai-exec " + s.Encode()
	cmdline, err := windows.UTF16FromString(line) // must be writable
	if err != nil {
		return 127, err
	}
	var pi windows.ProcessInformation
	r, _, e := procCreateProcessWithLogonW.Call(
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(c.User))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("."))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(pw))),
		logonWithProfile,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(c.Bin))),
		uintptr(unsafe.Pointer(&cmdline[0])),
		windows.CREATE_SUSPENDED|windows.CREATE_UNICODE_ENVIRONMENT,
		0, // the AI account's own environment; ai-exec adds the spec's
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(c.Home))),
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		return 127, fmt.Errorf("start as %s: %w", c.User, e)
	}
	defer windows.CloseHandle(pi.Process)
	defer windows.CloseHandle(pi.Thread)
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return 127, fmt.Errorf("job: %w", err)
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return 127, err
	}
	if _, err := windows.WaitForSingleObject(pi.Process, windows.INFINITE); err != nil {
		return 127, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(pi.Process, &code); err != nil {
		return 127, err
	}
	return int(code), nil
}

func protect(in []byte, on bool) ([]byte, error) {
	if len(in) == 0 {
		return nil, errors.New("empty")
	}
	src := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	ent := []byte("hpk-ai-account")
	entropy := windows.DataBlob{Size: uint32(len(ent)), Data: &ent[0]}
	var out windows.DataBlob
	var err error
	if on {
		err = windows.CryptProtectData(&src, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&src, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func readPassword(file string) (string, error) {
	blob, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	pw, err := protect(blob, false)
	if err != nil {
		return "", err
	}
	return string(pw), nil
}

func writePassword(file, pw string) error {
	blob, err := protect([]byte(pw), true)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, blob, 0o600)
}

type userInfo1 struct {
	Name, Password    *uint16
	PasswordAge, Priv uint32
	HomeDir, Comment  *uint16
	Flags             uint32
	ScriptPath        *uint16
}

const (
	userPrivUser       = 1
	ufScript           = 0x0001
	ufPasswdCantChange = 0x0040
	ufDontExpirePasswd = 0x10000
)

// Elevated reports whether this process runs with administrator rights.
func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

const userListKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon\SpecialAccounts\UserList`

// CreateAccount makes (or resets the password of) a standard local account
// for the AI and hides it from the sign-in screen. Needs administrator
// rights: `hpk ai-account` runs it in an elevated copy of hpk after the
// person confirmed. The password is made and kept on the person's side
// (an elevation may run as another administrator account, whose DPAPI the
// person could not read). It lets one start programs as the AI's account,
// nothing more; it is not a secret from the AI.
func CreateAccount(name, pw string) error {
	if !Elevated() {
		return errors.New("needs administrator rights")
	}
	u := userInfo1{Name: windows.StringToUTF16Ptr(name), Password: windows.StringToUTF16Ptr(pw), Priv: userPrivUser,
		Comment: windows.StringToUTF16Ptr("Hermes (the AI) runs as this account; made by hpk isolate"),
		Flags:   ufScript | ufPasswdCantChange | ufDontExpirePasswd}
	var perr uint32
	r, _, _ := procNetUserAdd.Call(0, 1, uintptr(unsafe.Pointer(&u)), uintptr(unsafe.Pointer(&perr)))
	if r == nerrUserExists {
		info := struct{ Password *uint16 }{windows.StringToUTF16Ptr(pw)}
		r, _, _ = procNetUserSetInfo.Call(0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(name))), 1003, uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&perr)))
	}
	if r != 0 {
		return fmt.Errorf("create account %s: %w", name, windows.Errno(r))
	}
	if k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, userListKey, registry.SET_VALUE); err == nil {
		_ = k.SetDWordValue(name, 0)
		k.Close()
	}
	return nil
}

// DeleteAccount removes the AI's account and its sign-in-screen entry.
func DeleteAccount(name string) error {
	if !Elevated() {
		return errors.New("needs administrator rights")
	}
	r, _, _ := procNetUserDel.Call(0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(name))))
	if r != 0 && r != nerrUserNotFound {
		return fmt.Errorf("delete account %s: %w", name, windows.Errno(r))
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, userListKey, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(name)
		k.Close()
	}
	return nil
}

// AccountExists reports whether a local account exists.
func AccountExists(name string) bool {
	var buf *byte
	if err := windows.NetUserGetInfo(nil, windows.StringToUTF16Ptr(name), 0, &buf); err != nil {
		return false
	}
	windows.NetApiBufferFree(buf)
	return true
}

func newPassword() string {
	b := make([]byte, 24)
	rand.Read(b)
	// meets any complexity policy: upper, lower, digit, symbol
	return "Hk7!" + base64.RawURLEncoding.EncodeToString(b)
}

// RunElevated runs hpk itself with args as administrator (Windows asks the
// person with its own prompt) and waits for it.
func RunElevated(args ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	var list []string
	for _, a := range args {
		list = append(list, windows.EscapeArg(a))
	}
	ps := "$p = Start-Process -FilePath " + q(self) + " -ArgumentList " + q(strings.Join(list, " ")) +
		" -Verb RunAs -Wait -PassThru -WindowStyle Hidden; exit $p.ExitCode"
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("the administrator step did not finish (%s)", msg)
	}
	return nil
}

// PersonSID is this account's SID, for icacls (*S-1-5-…).
func PersonSID() string {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return ""
	}
	return u.User.Sid.String()
}
