// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package scan looks at a project folder without an LLM (docs/DESIGN.md §8 step 3).
// It reads names, sizes and git metadata only; file contents are read later, and
// only for knowledge sources the person agreed to. Secrets are never opened.
package scan

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Source is a file that may hold project knowledge.
type Source struct {
	Path   string `json:"path"` // slash-separated, relative to the root
	Bytes  int64  `json:"bytes"`
	Kind   string `json:"kind"`             // readme | docs | notes | adr | changelog | guide | control | markdown
	Light  bool   `json:"light"`            // part of the light depth (README + docs)
	Tokens int    `json:"tokens,omitempty"` // estimate; exact after consent
}

// Skipped is a file hpk will never read, and why.
type Skipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Git is repository metadata. Counts only: no names or e-mails.
type Git struct {
	IsRepo       bool   `json:"is_repo"`
	Commits      int    `json:"commits,omitempty"`
	Contributors int    `json:"contributors,omitempty"`
	FirstCommit  string `json:"first_commit,omitempty"`
	LastCommit   string `json:"last_commit,omitempty"`
	Head         string `json:"head,omitempty"`
	Branches     int    `json:"branches,omitempty"`
}

// Result is the scan report.
type Result struct {
	Root      string         `json:"root"`
	Files     int            `json:"files"`
	Bytes     int64          `json:"bytes"`
	Languages map[string]int `json:"languages"` // files per language
	TopDirs   []string       `json:"top_dirs"`
	Sources   []Source       `json:"sources"`
	Skipped   []Skipped      `json:"skipped"`
	Git       Git            `json:"git"`
	TestCmd   string         `json:"test_cmd,omitempty"`
	BoardFile string         `json:"board_file,omitempty"`
	Worktrees string         `json:"worktrees,omitempty"`

	All []string `json:"-"` // every listed file (secrets excluded), for health checks
}

// Never opened, whatever the person picks (PRODUCT.md, §8).
var secretName = regexp.MustCompile(`(?i)(^|/)(\.env(\..*)?|.*\.(pem|key|p12|pfx|jks|keystore|kdbx)|id_(rsa|dsa|ecdsa|ed25519)(\.pub)?|\.npmrc|\.pypirc|\.netrc|\.git-credentials|credentials(\.json)?|secrets?(\..*)?|.*secret.*\.(json|ya?ml|toml|txt))$|(^|/)secrets?/`)

// Folders that are never project knowledge.
var noise = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, ".venv": true, "venv": true, "__pycache__": true, "target": true, ".next": true, ".idea": true, ".vscode": true, "worktrees": true, ".hermes": true}

var langByExt = map[string]string{
	".go": "Go", ".js": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript", ".ts": "TypeScript", ".tsx": "TypeScript", ".jsx": "JavaScript",
	".py": "Python", ".rb": "Ruby", ".rs": "Rust", ".java": "Java", ".kt": "Kotlin", ".swift": "Swift", ".cs": "C#", ".cpp": "C++", ".c": "C", ".h": "C",
	".php": "PHP", ".dart": "Dart", ".scala": "Scala", ".sql": "SQL", ".sh": "Shell", ".ps1": "PowerShell", ".html": "HTML", ".css": "CSS", ".scss": "CSS",
	".vue": "Vue", ".svelte": "Svelte", ".md": "Markdown", ".yaml": "YAML", ".yml": "YAML", ".json": "JSON", ".toml": "TOML",
}

var controlFiles = map[string]bool{"agents.md": true, "claude.md": true, "gemini.md": true, ".cursorrules": true, "soul.md": true, "copilot-instructions.md": true}

// Run scans root. It never reads file contents except tiny build manifests
// (package.json, Makefile) to detect the test command.
func Run(root string) (Result, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return Result{}, err
	}
	if !st.IsDir() {
		return Result{}, errors.New(abs + " is not a folder")
	}
	r := Result{Root: abs, Languages: map[string]int{}}
	g := gitInfo(abs)
	r.Git = g

	files, err := listFiles(abs, g.IsRepo)
	if err != nil {
		return r, err
	}
	top := map[string]bool{}
	for _, rel := range files {
		if secretName.MatchString(rel) {
			r.Skipped = append(r.Skipped, Skipped{Path: rel, Reason: "secret: never opened"})
			continue
		}
		info, err := os.Stat(filepath.Join(abs, filepath.FromSlash(rel)))
		if err != nil || info.IsDir() {
			continue
		}
		r.Files++
		r.All = append(r.All, rel)
		r.Bytes += info.Size()
		if lang := langByExt[strings.ToLower(path.Ext(rel))]; lang != "" {
			r.Languages[lang]++
		}
		if i := strings.IndexByte(rel, '/'); i > 0 {
			top[rel[:i]] = true
		}
		if s, ok := classify(rel, info.Size()); ok {
			r.Sources = append(r.Sources, s)
		}
	}
	for d := range top {
		r.TopDirs = append(r.TopDirs, d)
	}
	sort.Strings(r.TopDirs)
	sort.Slice(r.Sources, func(i, j int) bool { return r.Sources[i].Path < r.Sources[j].Path })
	r.TestCmd = detectTest(abs)
	for _, b := range []string{"BOARD.md", "board.md", "docs/BOARD.md"} {
		if _, err := os.Stat(filepath.Join(abs, b)); err == nil {
			r.BoardFile = b
			break
		}
	}
	if fi, err := os.Stat(filepath.Join(abs, "worktrees")); err == nil && fi.IsDir() {
		r.Worktrees = "worktrees/"
	}
	return r, nil
}

// classify decides whether a file is a knowledge source.
func classify(rel string, size int64) (Source, bool) {
	if size == 0 || size > 512<<10 { // empty, or too big to be prose
		return Source{}, false
	}
	low := strings.ToLower(rel)
	base := path.Base(low)
	ext := path.Ext(low)
	prose := ext == ".md" || ext == ".mdx" || ext == ".txt" || ext == ".rst" || ext == ".adoc"
	s := Source{Path: rel, Bytes: size, Tokens: int(size / 4)}
	switch {
	case controlFiles[base]:
		s.Kind = "control" // rules for agents: read for health checks, never rewritten
	case strings.HasPrefix(base, "readme") && prose:
		s.Kind, s.Light = "readme", true
	case (strings.HasPrefix(low, "docs/") || strings.HasPrefix(low, "doc/")) && prose:
		s.Kind, s.Light = "docs", true
	case (strings.Contains(low, "/adr/") || strings.HasPrefix(low, "adr/") || strings.Contains(low, "decisions/")) && prose:
		s.Kind = "adr"
	case strings.HasPrefix(low, "notes/") && prose:
		s.Kind = "notes"
	case strings.HasPrefix(base, "changelog") || strings.HasPrefix(base, "history"):
		s.Kind = "changelog"
	case strings.HasPrefix(base, "contributing") || strings.HasPrefix(base, "architecture") || strings.HasPrefix(base, "design"):
		s.Kind = "guide"
	case ext == ".md" || ext == ".mdx":
		s.Kind = "markdown"
	default:
		return Source{}, false
	}
	return s, true
}

// listFiles prefers git (respects .gitignore); otherwise walks and skips noise folders.
func listFiles(root string, isRepo bool) ([]string, error) {
	if isRepo {
		out, err := exec.Command("git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z").Output()
		if err == nil {
			var files []string
			for _, f := range strings.Split(string(out), "\x00") {
				if f != "" && !noise[strings.SplitN(f, "/", 2)[0]] {
					files = append(files, f)
				}
			}
			return files, nil
		}
	}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (noise[d.Name()] || strings.HasPrefix(d.Name(), ".")) && d.Name() != ".github" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

func gitInfo(root string) Git {
	if _, err := exec.LookPath("git"); err != nil {
		return Git{}
	}
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if top := run("rev-parse", "--show-toplevel"); top == "" {
		return Git{}
	}
	g := Git{IsRepo: true}
	g.Head = run("rev-parse", "HEAD")
	g.Commits, _ = strconv.Atoi(run("rev-list", "--count", "HEAD"))
	g.LastCommit = run("log", "-1", "--format=%cI")
	g.FirstCommit = run("log", "--reverse", "--format=%cI", "--max-parents=0", "-1")
	if lines := run("shortlog", "-sne", "HEAD"); lines != "" {
		g.Contributors = len(strings.Split(lines, "\n")) // counted, never stored
	}
	if lines := run("branch", "--format=%(refname:short)"); lines != "" {
		g.Branches = len(strings.Split(lines, "\n"))
	}
	return g
}

// detectTest guesses the project's test command from its build files.
func detectTest(root string) string {
	has := func(f string) bool { _, err := os.Stat(filepath.Join(root, f)); return err == nil }
	if raw, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil && len(raw) < 1<<20 {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(raw, &pkg) == nil && pkg.Scripts["test"] != "" {
			switch {
			case has("pnpm-lock.yaml"):
				return "pnpm test"
			case has("yarn.lock"):
				return "yarn test"
			case has("bun.lockb") || has("bun.lock"):
				return "bun test"
			}
			return "npm test"
		}
	}
	if f, err := os.Open(filepath.Join(root, "Makefile")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "test:") {
				return "make test"
			}
		}
	}
	switch {
	case has("go.mod"):
		return "go test ./..."
	case has("Cargo.toml"):
		return "cargo test"
	case has("pyproject.toml") || has("pytest.ini") || has("setup.py"):
		return "pytest"
	case has("pom.xml"):
		return "mvn test"
	case has("build.gradle") || has("build.gradle.kts"):
		return "./gradlew test"
	}
	return ""
}
