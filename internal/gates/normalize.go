// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package gates

import (
	"regexp"
	"strings"
)

// Command forms the rules must see through:
//   - subshells and groups: (rm -rf x), $(…), `…`, { …; }
//   - wrappers: sudo, exec, env A=b, nohup, time, command
//   - git's global options before the subcommand: git -C . push, git -c k=v push,
//     git --no-pager branch -D x, git --git-dir=… reset --hard
//
// candidates returns the command as typed plus these normalised forms; a rule
// that matches any of them matches the command.
func candidates(cmd string) []string {
	out := []string{cmd}
	flat := strings.NewReplacer("$(", " ", "(", " ", ")", " ", "`", " ", "{", " ", "}", " ").Replace(cmd)
	if flat != cmd {
		out = append(out, flat)
	}
	for _, c := range []string{cmd, flat} {
		if n := normGit(stripWrappers(c)); n != c {
			out = append(out, n)
		}
	}
	return out
}

var wrapperRE = regexp.MustCompile(`^(?i)\s*((sudo|exec|nohup|time|command|builtin|doas|xargs)(\s+-[a-z]+)*\s+|env(\s+[a-z_][a-z0-9_]*=\S*)+\s+|[a-z_][a-z0-9_]*=\S*\s+)`)

func stripWrappers(s string) string {
	for i := 0; i < 6; i++ {
		t := wrapperRE.ReplaceAllString(s, "")
		if t == s {
			break
		}
		s = t
	}
	return strings.TrimSpace(s)
}

// git options that come before the subcommand; those listed in withArg take one.
var gitWithArg = map[string]bool{"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true,
	"--exec-path": true, "--super-prefix": true, "--config-env": true, "--list-cmds": true}

func normGit(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return s
	}
	exe := strings.ToLower(strings.Trim(f[0], `"'`))
	exe = exe[strings.LastIndexAny(exe, `/\`)+1:]
	if exe != "git" && exe != "git.exe" {
		return s
	}
	i := 1
	for i < len(f) && strings.HasPrefix(f[i], "-") {
		opt := f[i]
		name := opt
		if j := strings.IndexByte(opt, '='); j > 0 {
			name = opt[:j]
		} else if gitWithArg[name] {
			i++ // its argument
		}
		i++
	}
	return strings.Join(append([]string{"git"}, f[min(i, len(f)):]...), " ")
}
