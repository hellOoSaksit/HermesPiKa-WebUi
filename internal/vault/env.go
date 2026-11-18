// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package vault

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func checkEnvFile(rel string) error {
	rel = filepath.ToSlash(rel)
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
		return errors.New("the .env file must be a path inside the project")
	}
	if !strings.HasPrefix(filepath.Base(rel), ".env") {
		return errors.New("only files named .env or .env.something")
	}
	return nil
}

// WriteEnv writes (or updates) the project's .env file(s) from the env secrets
// of that project: existing lines for other keys are kept, the vault's keys
// are set. Returns the files written. The file is recorded as managed, so
// hpk's guard stops the agent from reading it.
func (v *Vault) WriteEnv(home, project, root string) ([]string, error) {
	if root == "" {
		return nil, errors.New("this project has no folder")
	}
	v.mu.Lock()
	byFile := map[string]map[string]string{}
	for _, e := range v.entries {
		if e.Kind == "env" && e.Project == project {
			if byFile[e.EnvFile] == nil {
				byFile[e.EnvFile] = map[string]string{}
			}
			byFile[e.EnvFile][e.EnvKey] = e.Value
		}
	}
	v.mu.Unlock()
	if len(byFile) == 0 {
		return nil, errors.New("this project has no env secrets in the vault")
	}
	var written []string
	for rel, vals := range byFile {
		if err := checkEnvFile(rel); err != nil {
			return written, err
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		// never write through a link: it could point anywhere the AI can read
		if err := noLinks(root, path); err != nil {
			return written, err
		}
		var lines []string
		seen := map[string]bool{}
		if f, err := os.Open(path); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				l := sc.Text()
				k, _, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(l), "export "), "=")
				k = strings.TrimSpace(k)
				if val, mine := vals[k]; ok && mine {
					lines = append(lines, k+"="+quote(val))
					seen[k] = true
					continue
				}
				lines = append(lines, l)
			}
			f.Close()
		}
		keys := make([]string, 0, len(vals))
		for k := range vals {
			if !seen[k] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) > 0 && len(lines) > 0 {
			lines = append(lines, "", "# from the hpk vault")
		} else if len(keys) > 0 {
			lines = append(lines, "# from the hpk vault (hpk keeps the values; edit them in Console > Personal > Vault)")
		}
		for _, k := range keys {
			lines = append(lines, k+"="+quote(vals[k]))
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			return written, err
		}
		written = append(written, rel)
		addManaged(home, path)
	}
	sort.Strings(written)
	return written, nil
}

func quote(v string) string {
	if v == "" || strings.ContainsAny(v, " \t#\"'$`\\") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(v) + `"`
	}
	return v
}

func managedPath(home string) string { return filepath.Join(home, "hpk", "vault", "managed-env.json") }

func addManaged(home, path string) {
	list := Managed(home)
	abs, _ := filepath.Abs(path)
	for _, p := range list {
		if p == abs {
			return
		}
	}
	list = append(list, abs)
	b, _ := json.Marshal(list)
	_ = os.WriteFile(managedPath(home), b, 0o600)
}

// Managed lists the .env files hpk wrote from the vault (absolute paths).
// The hook reads this without opening the vault itself.
func Managed(home string) []string {
	var list []string
	if raw, err := os.ReadFile(managedPath(home)); err == nil {
		_ = json.Unmarshal(raw, &list)
	}
	return list
}

// TouchesManaged reports whether a tool call names a managed .env file.
func TouchesManaged(home, cwd string, values ...string) (string, bool) {
	list := Managed(home)
	if len(list) == 0 {
		return "", false
	}
	for _, m := range list {
		base := filepath.Base(m)
		dir := filepath.Dir(m)
		for _, v := range values {
			if v == "" || !strings.Contains(strings.ToLower(v), strings.ToLower(base)) {
				continue
			}
			v = foldIfWindows(v)
			m, dir, base := foldIfWindows(m), foldIfWindows(dir), foldIfWindows(base)
			// a path argument: resolve it
			p := v
			if !filepath.IsAbs(p) && cwd != "" {
				p = filepath.Join(cwd, p)
			}
			if abs, err := filepath.Abs(p); err == nil && strings.EqualFold(abs, m) {
				return m, true
			}
			// a command line: the file name with the folder as cwd, or the full path in it
			if strings.Contains(v, m) || strings.Contains(filepath.ToSlash(v), filepath.ToSlash(m)) {
				return m, true
			}
			if cwd != "" && strings.EqualFold(filepath.Clean(cwd), dir) && containsWord(v, base) {
				return m, true
			}
		}
	}
	return "", false
}

func containsWord(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || strings.ContainsRune(" \t'\"=<>|;&(/", rune(s[j-1]))
		end := j + len(w)
		after := end == len(s) || strings.ContainsRune(" \t'\"<>|;&)", rune(s[end]))
		if before && after {
			return true
		}
		i = j + 1
	}
}

// ErrManaged is the message the agent sees.
func ErrManaged(path string) string {
	return fmt.Sprintf("%s is written by hpk from the person's vault; its values are not for the AI. Use the variable names only (the program reads the file itself).", filepath.Base(path))
}

// noLinks refuses a path whose file or any folder below root is a link.
func noLinks(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errors.New("the .env file must be inside the project")
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a link; hpk writes secrets only into real files", filepath.ToSlash(rel))
		}
	}
	return nil
}

func foldIfWindows(s string) string {
	if os.PathSeparator == 92 { // '\\' (Windows)
		return strings.ToLower(s)
	}
	return s
}
