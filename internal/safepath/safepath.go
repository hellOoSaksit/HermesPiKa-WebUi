// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package safepath is the one place that decides which project-relative paths
// hpk may read, write or move as the person, and how it reaches them: every
// component is a plain name (Name), and every folder on the way is checked with
// Lstat, so a link or a Windows junction the AI planted is never followed.
//
// filepath.EvalSymlinks is not enough on Windows: since Go 1.23 it leaves
// junctions (mount points) unresolved, so a check built on it passes a
// junction that leads anywhere.
package safepath

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// secretRE: key and credential file names hpk never serves, writes or moves.
var secretRE = regexp.MustCompile(`(?i)(^id_[a-z0-9_]+(\.pub)?$|\.(pem|key|p12|pfx|kdbx|keystore|jks|env|tfstate|tfvars|ppk|asc|gpg)$|\.tfstate\.|^credentials|^service-?account.*\.json$|secrets?\.(json|ya?ml|toml)$|^\.?(npmrc|pypirc|netrc|pgpass|htpasswd)$)`)

// shortRE: a Windows 8.3 alias (GIT~1, PROGRA~1), which can name a hidden folder.
var shortRE = regexp.MustCompile(`~[0-9]`)

// Secret reports a key or credential file name.
func Secret(name string) bool { return secretRE.MatchString(name) }

// ReservedDevice reports Windows device names (CON, NUL, COM1, AUX.txt …);
// refused on every OS so a project folder stays usable on Windows.
func ReservedDevice(part string) bool {
	base := strings.ToUpper(part)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, " ")
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9'
}

// Name reports why one path component may not be used, or nil: empty, . and
// .., hidden names, control characters, characters Windows reserves (including
// the : of an alternate stream), names Windows trims (a trailing dot or
// space), device names, 8.3 aliases, and key or credential files.
func Name(part string) error {
	switch {
	case part == "" || part == "." || part == "..":
		return errors.New("give a plain path inside the project")
	case strings.HasPrefix(part, "."):
		return errors.New("hidden files and folders are not shown")
	case strings.ContainsFunc(part, func(r rune) bool { return r < 32 || r == 127 }):
		return errors.New("that name has control characters")
	case strings.ContainsAny(part, `/\:*?"<>|`):
		return errors.New("that name is not allowed")
	case strings.HasSuffix(part, ".") || strings.HasSuffix(part, " "):
		return errors.New("give the file's plain path")
	case ReservedDevice(part):
		return errors.New("that name is reserved on Windows")
	case shortRE.MatchString(part):
		return errors.New("give the file's plain path, not its short name")
	case Secret(part):
		return errors.New("key and credential files are not shown")
	}
	return nil
}

// Rel checks a project-relative path with forward slashes: not absolute, and
// every component passes Name.
func Rel(rel string) error {
	if rel == "" || strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return errors.New("give a plain path inside the project")
	}
	for _, part := range strings.Split(rel, "/") {
		if err := Name(part); err != nil {
			return fmt.Errorf("%s: %w", part, err)
		}
	}
	return nil
}

// ErrLink: a link or junction is on the way.
var ErrLink = errors.New("a link or junction is on the way; hpk does not follow it")

// IsLink reports whether fi, from os.Lstat(path), is a symbolic link, a
// junction or another reparse point that sends a path somewhere else.
func IsLink(path string, fi fs.FileInfo) bool {
	if fi.Mode()&fs.ModeSymlink != 0 {
		return true
	}
	return redirects(path, fi)
}

// Root resolves the project folder itself (the person chose it, so a link
// there is followed) and checks it is a folder.
func Root(root string) (string, error) {
	if root == "" {
		return "", errors.New("this project has no folder")
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return "", errors.New("the project folder is missing")
	}
	return real, nil
}

// Dir returns root/rel, walking down from root one component at a time: each
// must pass Name and be a real folder (Lstat; no link, no junction). With
// create, missing folders are made on the way. A missing folder without create
// is an error that wraps fs.ErrNotExist. rel "" is root.
func Dir(root, rel string, create bool) (string, error) {
	if rel == "" {
		return root, nil
	}
	if err := Rel(rel); err != nil {
		return "", err
	}
	cur := root
	for _, part := range strings.Split(rel, "/") {
		next := filepath.Join(cur, part)
		fi, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) && create {
			if err := os.Mkdir(next, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
				return "", err
			}
			fi, err = os.Lstat(next)
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("no such folder in the project: %w", fs.ErrNotExist)
			}
			return "", err
		}
		if IsLink(next, fi) {
			return "", ErrLink
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("%s is not a folder", part)
		}
		cur = next
	}
	return cur, nil
}

// File returns root/rel for a regular file reached without links (see Dir),
// with its Lstat info.
func File(root, rel string) (string, fs.FileInfo, error) {
	if err := Rel(rel); err != nil {
		return "", nil, err
	}
	dirRel, name := "", rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		dirRel, name = rel[:i], rel[i+1:]
	}
	dir, err := Dir(root, dirRel, false)
	if err != nil {
		return "", nil, err
	}
	full := filepath.Join(dir, name)
	fi, err := os.Lstat(full)
	if err != nil {
		return "", nil, errors.New("no such file in the project")
	}
	if IsLink(full, fi) {
		return "", nil, ErrLink
	}
	if !fi.Mode().IsRegular() {
		return "", nil, errors.New("not a regular file")
	}
	return full, fi, nil
}
