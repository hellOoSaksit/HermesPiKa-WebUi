// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package launch

import (
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Start at login is the one part of hpk that is different on every OS. It is
// per user, needs no administrator rights, and runs `hpk serve` in the
// background (no window, no browser): the person opens the Console when they
// want it, or the installed app.
//
//	Windows  a Task Scheduler task "hpk" with a logon trigger for this user,
//	         run through `conhost --headless` so no console window appears
//	macOS    ~/Library/LaunchAgents/local.hpk.serve.plist (launchd, RunAtLoad)
//	Linux    ~/.config/systemd/user/hpk.service (systemd --user, enabled)

// Autostart describes one person's start-at-login entry.
type Autostart struct {
	GOOS    string
	Exe     string // the hpk binary to run
	Home    string // the person's home folder
	DataDir string // hpk's own folder (logs)
	User    string // Windows: DOMAIN\user for the logon trigger
	UID     int    // macOS: launchctl domain gui/<uid>
}

// TaskName is the Windows task; Label the macOS job; Unit the systemd unit.
const (
	TaskName = "hpk"
	Label    = "local.hpk.serve"
	Unit     = "hpk.service"
)

// ThisAutostart fills Autostart for the running program and person.
func ThisAutostart() (Autostart, error) {
	exe, err := os.Executable()
	if err != nil {
		return Autostart{}, err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	home, _ := os.UserHomeDir()
	a := Autostart{GOOS: runtime.GOOS, Exe: exe, Home: home, DataDir: DataDir(), UID: os.Getuid()}
	if runtime.GOOS == "windows" {
		a.User = os.Getenv("USERDOMAIN") + `\` + os.Getenv("USERNAME")
	}
	return a, nil
}

// Supported says whether this OS has a start-at-login method in hpk.
func (a Autostart) Supported() bool {
	switch a.GOOS {
	case "windows", "darwin", "linux":
		return true
	}
	return false
}

// File is the definition hpk writes: the task XML, plist or unit.
func (a Autostart) File() (path, content string) {
	switch a.GOOS {
	case "windows":
		return filepath.Join(a.DataDir, "autostart", "hpk-task.xml"), a.taskXML()
	case "darwin":
		return filepath.Join(a.Home, "Library", "LaunchAgents", Label+".plist"), a.plist()
	default:
		return filepath.Join(a.configHome(), "systemd", "user", Unit), a.unit()
	}
}

func (a Autostart) configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" && a.GOOS == runtime.GOOS {
		return d
	}
	return filepath.Join(a.Home, ".config")
}

// EnableCommands are run after writing File; a leading "-" means a failure is fine.
func (a Autostart) EnableCommands() [][]string {
	path, _ := a.File()
	switch a.GOOS {
	case "windows":
		return [][]string{{"schtasks", "/Create", "/TN", TaskName, "/XML", path, "/F"}}
	case "darwin":
		// bootout first so a changed plist is reloaded; its error (not loaded) is ignored.
		return [][]string{{"-launchctl", "bootout", "gui/" + strconv.Itoa(a.UID), path}, {"launchctl", "bootstrap", "gui/" + strconv.Itoa(a.UID), path}}
	default:
		return [][]string{{"systemctl", "--user", "daemon-reload"}, {"systemctl", "--user", "enable", Unit}}
	}
}

// DisableCommands are run before removing File.
func (a Autostart) DisableCommands() [][]string {
	path, _ := a.File()
	switch a.GOOS {
	case "windows":
		return [][]string{{"-schtasks", "/Delete", "/TN", TaskName, "/F"}}
	case "darwin":
		return [][]string{{"-launchctl", "bootout", "gui/" + strconv.Itoa(a.UID), path}}
	default:
		return [][]string{{"-systemctl", "--user", "disable", Unit}}
	}
}

// Enabled reports whether the entry exists.
func (a Autostart) Enabled() bool {
	if a.GOOS == "windows" {
		return exec.Command("schtasks", "/Query", "/TN", TaskName).Run() == nil
	}
	path, _ := a.File()
	_, err := os.Stat(path)
	return err == nil
}

// Enable writes the definition and registers it with the OS.
func (a Autostart) Enable() error {
	if !a.Supported() {
		return errors.New("start at login is not available on " + a.GOOS)
	}
	path, content := a.File()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data := []byte(content)
	if a.GOOS == "windows" {
		data = utf16LE(content) // schtasks reads task XML as UTF-16
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Join(a.DataDir, "logs"), 0o700)
	return runAll(a.EnableCommands())
}

// Disable unregisters and removes the definition.
func (a Autostart) Disable() error {
	if !a.Supported() {
		return nil
	}
	err := runAll(a.DisableCommands())
	path, _ := a.File()
	if e := os.Remove(path); e != nil && !os.IsNotExist(e) && err == nil {
		err = e
	}
	return err
}

func runAll(cmds [][]string) error {
	for _, c := range cmds {
		name, soft := c[0], false
		if strings.HasPrefix(name, "-") {
			name, soft = name[1:], true
		}
		out, err := exec.Command(name, c[1:]...).CombinedOutput()
		if err != nil && !soft {
			return fmt.Errorf("%s: %v: %s", strings.Join(append([]string{name}, c[1:]...), " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (a Autostart) taskXML() string {
	x := html.EscapeString
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>HPK Console (hpk serve) for this user. Turn off in HPK Console, Settings, or: schtasks /Delete /TN hpk /F</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + x(a.User) + `</UserId>
      <Delay>PT15S</Delay>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + x(a.User) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Hidden>true</Hidden>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>conhost.exe</Command>
      <Arguments>--headless "` + x(a.Exe) + `" serve</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

func (a Autostart) plist() string {
	x := html.EscapeString
	logf := filepath.Join(a.DataDir, "logs", "serve.log")
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + Label + `</string>
  <key>ProgramArguments</key>
  <array><string>` + x(a.Exe) + `</string><string>serve</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><false/>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>` + x(logf) + `</string>
  <key>StandardErrorPath</key><string>` + x(logf) + `</string>
</dict>
</plist>
`
}

func (a Autostart) unit() string {
	q := func(s string) string { // systemd quoting for ExecStart
		if strings.ContainsAny(s, " \t\"\\") {
			return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
		}
		return s
	}
	return `[Unit]
Description=HPK Console (hpk serve) for this user

[Service]
Type=simple
ExecStart=` + q(a.Exe) + ` serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`
}

// utf16LE encodes s with a byte-order mark, as schtasks /XML expects.
func utf16LE(s string) []byte {
	out := []byte{0xFF, 0xFE}
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			hi, lo := 0xD800+(r>>10), 0xDC00+(r&0x3FF)
			out = append(out, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
			continue
		}
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}
