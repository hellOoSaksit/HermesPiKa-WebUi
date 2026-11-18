# File Locations, Folder Rules and Solo-First Console — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per-project folder rules that the AI asks for once and keeps in the wiki (used for AI context, uploads, and a "tidy" button). A folder picker in the Console. Movable wiki and recording locations. Team features greyed out for v1.

**Architecture:**
- A new `internal/folders` package parses the rules table from the wiki page `decisions/folders.md`. It trusts the page only if its hash matches one hpk recorded at approval time, in `<home>/hpk/trusted/`. That folder is read-only for the AI account when the AI is isolated.
- `internal/relocate` moves folders safely.
- The serve APIs (dirs, upload, list, tidy, locations) and the hooks consume both packages.
- The Console gets a folder picker, a Files view and a `features.team` flag.

**Tech Stack:** Go 1.2x (stdlib, `golang.org/x/sys/windows`), vanilla-JS Console (`assets/console`, `h()` helpers, `t()` i18n), node test runner for `tools/*.test.mjs`.

**Spec:** `docs/superpowers/specs/2026-11-18-file-locations-design.md`

**Change from spec (planner's call):**
- **Wiki locations:** stored in `<home>/hpk/trusted/locations.json`, not `<hpk data>/wikis.json`. The AI-side hooks and `hpk wiki` (run as `hpk-ai`) must be able to *read* the location. `hpk/trusted/` gives that: readable by the AI, writable only by the person when isolated.
- **Recording location:** always stored in `<hpk data>/settings.json`. That is the person's own AppData, which other accounts cannot open. So it is person-only both with and without isolation.

## Global Constraints

- Every new source file starts with `// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0` (JS: same line).
- Every Console string goes through `t()`, with keys added to both `assets/console/i18n/en.json` and `th.json`.
- No real personal names, paths or keys in code, tests or docs. Use `orbit-app`, `C:\code\app`, `/home/me` style.
- Folder-rules page path: `decisions/folders.md`. Table header, verbatim: `| Folder | What goes here | Extensions |`.
- Rules are used only when the page's SHA-256 equals the trusted hash. Otherwise state `changed`, and the Console shows "the rules page was changed outside approval".
- hpk never moves or deletes a person's file without a confirm click. Approving rules only *creates* missing folders.
- Upload limit: 2 GB per file by default (`files.upload_max` in project.yaml). Name clash → `name (2).ext`. Never overwrite.
- Hook time budget: unchanged (Windows 80 ms, see `internal/hook` latency test).
- `GET /api/fs/dirs` requires the Console key even though it is a GET. Solo mode only.
- Team flag: `features.team` is false by default. `HPK_FEATURES=team` turns it on. `hpk serve --team` is unaffected.
- Run before each commit: `go vet ./...`, `go test ./<touched pkgs>`, `node --test tools/*.test.mjs` when Console JS changed.
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Work on branch `feat/file-locations`.

## Review Focus

1. **Folder names with spaces or Thai, file names with Thai or spaces** (`RAW 3D`, `ฉาก 1.blend`): ensure, upload, list and tidy all keep them intact. → tests in Tasks 1, 5 and 6.
2. **Extension case and compound extensions** (`.FBX`, `scene.blend1`, `a.tar.gz`): match case-insensitively on the last extension only. `.blend1` matches only if listed. → Task 1 test.
3. **A table a person wrote by hand**: alignment colons (`|:---|`), extra spaces, a 4th column, CRLF line endings. Parse it, and never drop rows silently. → Task 1 test.
4. **An upload interrupted mid-stream**: no partial file is left in the project. → Task 5 test.
5. **Tidy in a folder that is not a git repo, or with git missing from PATH**: fall back to a plain rename and do not fail. → Task 6 test.

---

### Task 1: `internal/folders`: rules, trust, ensure

**Files:**
- Create: `internal/folders/rules.go`, `internal/folders/trust.go`, `internal/folders/folders_test.go`
- Modify: `cmd/hpk/wiki.go:186` (after `w.Approve`), `internal/serve/api.go:186` (after `wk.Approve`), `cmd/hpk/wiki.go` lint command (append folder findings)
- Modify: `cmd/hpk/isolate.go` setup options (add `<home>/hpk/trusted` to `ReadOnly`; `os.MkdirAll` it before `SetupPlan`)

**Interfaces:**
- Produces:
  - `const Page = "decisions/folders.md"`
  - `type Rule struct { Folder, What string; Exts []string }`. `Exts` is lower-case with a leading dot.
  - `type Rules struct { Rows []Rule; Answered bool; Bad []string }`. `Answered` is true when the page exists, even with no rows (meaning "no split"). `Bad` holds one message per rejected line.
  - `func Parse(body string) Rules`
  - `func (r Rules) For(name string) (folder string, ok bool)`: by the last extension, case-insensitive. The first matching row wins.
  - `type State string`, with consts `None="none"`, `Pending="pending"`, `OK="ok"`, `Changed="changed"`
  - `func Load(home string, p *project.Project) (Rules, State)`
    - `Pending` = there is no approved page, but a draft whose `Meta.Target == Page` exists.
    - `Changed` = a page exists but its hash ≠ the trusted hash, or no trusted hash exists.
    - The returned Rules are usable only when the state is `OK`.
  - `func TrustedDir(home string) string` = `<home>/hpk/trusted`
  - `func Trust(home string, p *project.Project) error`: writes the hex SHA-256 of the page bytes to `TrustedDir/folders-<project>.sha256`.
  - `func Ensure(root string, r Rules) (created []string, err error)`: `MkdirAll` for each missing `Rule.Folder`. Refuses `..` and absolute paths.
  - `func AfterApprove(home string, p *project.Project, res []wiki.Result) error`: if any `res` has `Target == Page && Done == "approved"`, run `Trust`, then `Ensure(p.Root, …)`, then `project.Audit`.

- [ ] **Step 1: Write failing tests** in `folders_test.go`:
  - `TestParseTable`:
    - Input: the header plus rows `RAW 3D | งานต้นฉบับ | .blend .max`, `Export | x | .FBX, obj`, `Art/Texture | x | png`.
    - Rows: 3. `Exts` of row 2 = `[".fbx", ".obj"]`. Row 3 = `[".png"]`.
    - `For("ฉาก 1.FBX")` = `("Export", true)`. `For("a.tar.gz")` = `false`.
  - `TestParseHandWritten`:
    - Input: alignment row `|:---|---:|:-:|`, a 4th column, CRLF.
    - Rows: 2. `Bad` is empty.
  - `TestParseRejects`:
    - Input: rows `../out | x | .a`, `C:\abs | x | .b`, `X | x | .a` (duplicate ext).
    - Rows: 1 (`X`). `len(Bad) == 3`. Using the first row for `.a` would be wrong, because `../out` is rejected, so `.a` maps to `X`.
  - `TestNoSplitIsAnAnswer`: page with header only → `Answered && len(Rows)==0`.
  - `TestTrustAndState`:
    - With no page → `None`.
    - After `CreateDraft(Target: Page)` → `Pending`.
    - Approve via `wiki.Approve`, then `AfterApprove` → `OK`, and the folder `RAW 3D` exists under the root.
    - Rewrite the page file directly → `Changed`.
    - Create a page directly, without approving → `Changed`.
- [ ] **Step 2:** Run `go test ./internal/folders/`. Expected: FAIL (package missing).
- [ ] **Step 3:** Implement `rules.go` and `trust.go`.
  - Parse: find the first line whose cells (trimmed, case-insensitive) equal the 3 header names. Skip the alignment row. Split on `|`. Folder = `filepath.ToSlash(strings.TrimSpace(cell))`. Exts are split on `[\s,]+`, with a dot added if missing.
- [ ] **Step 4:** Wire `AfterApprove` after both `Approve` calls. Errors are logged to stderr or the response, and never fail the approval. Wiki lint: when `Load` gives `Bad`, print `decisions/folders.md: <msg>`. Isolation: add the trusted dir to `ReadOnly`.
- [ ] **Step 5:** Run `go test ./internal/folders/ ./internal/wiki/ ./internal/serve/ ./cmd/hpk/`. Expected: PASS.
- [ ] **Step 6: Commit** `folders: rules page, trusted hash, create folders on approval`

### Task 2: `internal/relocate` + movable wiki and project root

**Files:**
- Create: `internal/relocate/relocate.go`, `internal/relocate/relocate_test.go`, `internal/serve/locationsapi.go`, `internal/serve/locationsapi_test.go`
- Modify: `internal/project/project.go:45` (`WikiDir`), `internal/isolate/isolate.go` (exported grant helpers), `internal/isolate/plan_windows.go`, `internal/isolate/user_other.go`

**Interfaces:**
- Consumes: `folders.TrustedDir(home)`.
- Produces:
  - `func relocate.Move(src, dst string) error`
    - `dst` must not exist or must be empty. Not inside `src`, and not the reverse.
    - Order: copy the tree (with links refused), compare file count and sizes, rename `src` → `src.hpk-old`, write the new location, then remove `src.hpk-old`.
    - On any failure before the swap, remove the partial `dst` and leave `src` untouched.
  - `func project.WikiLocations(home string) map[string]string` reads `<home>/hpk/trusted/locations.json` `{"wikis":{"<name>":"<abs dir>"}}`.
  - `(p *Project) WikiDir()` returns `WikiLocations(HomeOf(p))[p.Name]` when that is set, else `p.Dir/wiki`.
  - `func project.SetWikiLocation(home, name, dir string) error`
  - `func isolate.Grant(c *Config, dir string) error` and `func isolate.Ungrant(c *Config, dir string) error`. Windows: `icacls (OI)(CI)M` and `/remove:g`. They update `c.Granted` and call `Save`. Docker mode and other OSes return `errors.New("not supported while the AI runs in a container")` / the existing `errManual`.
  - HTTP:
    - `POST /api/project/wiki-dir {project, dir}` → `{ok, dir}`
    - `POST /api/project/root {project, dir}` → `{ok}`
    - Both are solo only and need the Console key (POST).

- [ ] **Step 1: Failing tests**:
  - `TestMoveKeepsSourceOnFailure`: make `dst` a non-empty dir → error. `src` is unchanged and there is no `src.hpk-old`.
  - `TestMoveTree`: a nested tree with a `.git` dir and Thai file names → all present at `dst`, and `src` is gone.
  - `TestWikiDirOverride`: `SetWikiLocation`, then `Load(home,name).WikiDir()` = the new dir.
  - `TestWikiDirAPI`:
    - The POST moves the wiki and `wiki.Open(p.WikiDir()).Exists()`.
    - A relative `dir` → 400.
    - A `dir` inside another project's wiki → 400.
  - `TestRootAPI`: a missing dir → 400. An existing dir → `project.yaml` root updated.
- [ ] **Step 2:** Run `go test ./internal/relocate/ ./internal/project/ ./internal/serve/ -run 'Move|WikiDir|Root'`. Expected: FAIL.
- [ ] **Step 3:** Implement.
  - Wiki move: refuse while a session of this project is streaming. The serve session manager already knows: use its busy check. If none exists, add `func (s *Server) projectBusy(name string) bool` over the ACP sessions linked via `project.Sessions`.
  - When `isolate.Load()` is ok, call `Grant(new)` and `Ungrant(old)`. That applies to both the root change and the wiki move.
  - Also make `locations.json` read-only for the AI: it lives in `hpk/trusted`, which is already ReadOnly from Task 1.
- [ ] **Step 4:** Run the same tests. Expected: PASS.
- [ ] **Step 5: Commit** `wiki and project root can move; isolation grants follow`

### Task 3: movable browser recordings

**Files:**
- Modify: `internal/browser/record.go:60,150` (use `RunsDir`), `internal/serve/browserapi.go` (new route), `internal/isolate/setup.go` (setup/teardown steps), `internal/isolate/isolate.go` (export `MakePrivate`)
- Test: `internal/browser/record_test.go` (new), `internal/serve/vaultapi_test.go` or a new `recordingsapi_test.go`

**Interfaces:**
- Consumes: `relocate.Move`, `launch.DataDir()`, `isolate.Load`, `isolate.MakePrivate(dir string) error` (wraps the existing `makePrivate`).
- Produces:
  - `func browser.RunsDir(home string) string`: `<hpk data>/settings.json` `{"recordings":"<abs>"}` if set, else `filepath.Join(Dir(home),"runs")`.
  - `func browser.SetRunsDir(dir string) error`
  - `POST /api/browser/recordings-dir {dir}` → `{ok, dir}`. `dir:""` resets to the default.

- [ ] **Step 1: Failing tests**:
  - `TestRunsDirSetting`: `HPK_DATA_DIR` = temp. Set → `RunsDir` returns it. Reset → default.
  - `TestRecordingsDirRules`, with isolation config present (via `HPK_ISOLATION_FILE`):
    - A dir under the Hermes home → 400.
    - A dir under a project root → 400.
    - Otherwise OK, and recordings are moved.
    - With the browser running → 409.
  - `TestIsolateSetupTakesBackReadableRecordings`:
    - Precondition: `settings.json` points under a project root.
    - `SetupPlan` steps include one with `Say` containing `"browser recordings"`.
    - After that step runs, `RunsDir` = the private default.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implement.
  - After a move under isolation, call `MakePrivate(dir)`.
  - Setup adds the move-back step only when the custom dir is under the home or a project root.
  - Teardown leaves a custom dir alone.
- [ ] **Step 4:** Run `go test ./internal/browser/ ./internal/isolate/ ./internal/serve/`. Expected: PASS (the real-browser test may be skipped with `HPK_SKIP_BROWSER=1`).
- [ ] **Step 5: Commit** `browser: recordings location can change; stays private under isolation`

### Task 4: folder picker (`GET /api/fs/dirs`) + Console component

**Files:**
- Create: `internal/serve/fsapi.go`, `internal/serve/fsapi_test.go`, `assets/console/js/folderpick.js`
- Modify:
  - `internal/serve/consolekey.go` (key required for `GET /api/fs/`)
  - `assets/console/js/views/wizard.js:43,238` (Browse buttons)
  - `assets/console/js/views/settings.js` (project root and wiki location rows calling Task 2's APIs)
  - Browser settings (recordings location, Task 3's API)
  - `assets/console/js/data.js` / `live.js` (api methods)
  - i18n en/th

**Interfaces:**
- Consumes: Task 2/3 endpoints.
- Produces:
  - `GET /api/fs/dirs?path=` → `{path, parent, dirs:[{name, path}], places:[{label, path}]}`. Folders only, sorted case-insensitively.
    - Hidden entries: names starting with `.`, plus `C:\Windows`, `C:\Program Files*`, `C:\ProgramData`, `/proc`, `/sys`, `/dev`, the hpk private folder and `<home>/hpk/trusted`.
    - Empty `path` → the places list only: Home, Documents, Desktop, and on Windows the existing drive letters.
  - JS: `export async function pickFolder({ title, start }) → Promise<string|null>` (an `openDrawer`-based dialog: breadcrumbs, list, "Use this folder", "New folder" omitted (YAGNI)).
  - `api.listDirs(path)`, `api.setWikiDir(project, dir)`, `api.setProjectRoot(project, dir)`, `api.setRecordingsDir(dir)`.

- [ ] **Step 1: Failing tests** (`fsapi_test.go`):
  - `TestDirsNeedsConsoleKey`: no cookie → 401 with `X-HPK-Need-Key`.
  - `TestDirsListsFoldersOnly`: a temp dir with `a/`, `b.txt`, `.hid/` → `dirs == ["a"]`.
  - `TestDirsHidesPrivate`: the private dir is not listed under its parent.
  - `TestDirsTeamMode404`: a team-mode server → 404.
- [ ] **Step 2:** Run `go test ./internal/serve/ -run Dirs`. Expected: FAIL.
- [ ] **Step 3:** Implement the API and key rule. Then the JS component, with keyboard support (↑↓ Enter Backspace=parent), `data-tip` on truncated paths, and the existing tokens and classes in `console.css`.
- [ ] **Step 4:**
  - Run `go test ./internal/serve/` and `node --test tools/*.test.mjs`. Expected: PASS.
  - Manual check with `HPK_CONSOLE_DIR=assets/console`:
    - Wizard → Browse → pick → the path input is filled.
    - Settings → wiki location → move works, and the toast shows the new path.
- [ ] **Step 5: Commit** `console: folder picker; change project folder, wiki and recordings locations`

### Task 5: upload + file list API, chat attachments upload for real

**Files:**
- Create: `internal/serve/uploadapi.go`, `internal/serve/uploadapi_test.go`
- Modify:
  - `internal/serve/filesapi.go` (add list route; reuse `projectFile` / `allowedParts`)
  - `assets/console/js/views/chat.js:394-450` (upload on attach, progress, send relative paths)
  - `assets/console/js/live.js` (api)
  - i18n

**Interfaces:**
- Consumes: `folders.Load`, `Rules.For`.
- Produces:
  - `POST /api/files/upload?project=` (multipart, field `file`, repeatable) → `{files:[{name, path, folder}]}`, where `path` is project-relative with forward slashes.
    - Destination: `Rules.For(name)` when the state is `OK`, else the project root.
    - `MaxBytesReader` per file at `files.upload_max` (default `2<<30`).
    - Write to `<dest>/.hpk-upload-*` and rename at the end. On any error, remove the temp file.
    - Name clash → `name (2).ext`, `name (3).ext`…
    - Refuse names failing `allowedParts`, names with a separator, `.env` / `.env.*`, and writes through links.
  - `GET /api/files/list?project=&path=` → `{path, entries:[{name, dir, size, mtime, folder}]}`. `folder` = the rule folder this file belongs in (empty if none). Hidden entries are excluded.
  - JS: `api.uploadFiles(project, File[] , onProgress) → Promise<{files}>` (XHR for progress), `api.listFiles(project, path)`.

- [ ] **Step 1: Failing tests**:
  - `TestUploadFollowsRules`: rules approved (helpers from the Task 1 tests) → `ฉาก 1.FBX` lands in `Export/ฉาก 1.FBX`, and `notes.md` lands in the root.
  - `TestUploadNoRulesGoesToRoot`.
  - `TestUploadNeverOverwrites`: uploading the same name twice → the second is `x (2).fbx`.
  - `TestUploadInterruptedLeavesNothing`: the body is cut mid-file (a reader that errors after N bytes) → error, and no `.hpk-upload-*` or partial file remains.
  - `TestUploadRefuses`: `../a`, `.env`, a name over the size limit (set `files.upload_max: 10`) → 400 or 413.
  - `TestListMarksRuleFolder`.
- [ ] **Step 2:** Run `go test ./internal/serve/ -run 'Upload|List'`. Expected: FAIL.
- [ ] **Step 3:** Implement. Chat: attaching uploads at once. The chip shows a progress bar, then `✓`. The message sends `attachments` as the returned relative paths. Chat attachments are disabled when there is no live project.
- [ ] **Step 4:** Run the tests and `node --test tools/*.test.mjs`. Expected: PASS.
- [ ] **Step 5: Commit** `files: upload into the folder the rules name; chat attachments upload`

### Task 6: tidy: plan, apply, undo

**Files:**
- Create: `internal/folders/tidy.go`, `internal/folders/tidy_test.go`, `internal/serve/tidyapi.go`, `internal/serve/tidyapi_test.go`

**Interfaces:**
- Consumes: `Rules`, `project.Audit`.
- Produces:
  - `type Move struct { From, To string; Size int64; MTime time.Time }`. Paths are project-relative with forward slashes.
  - `type Plan struct { Moves []Move; Homeless []string }`
  - `func PlanTidy(root string, r Rules) (Plan, error)`
    - Skips `.git`, `node_modules`, hidden entries, git-ignored paths (`git check-ignore --stdin`; when git is missing or this is not a repo, nothing is ignored), and files already anywhere under their rule folder.
    - `Homeless` = files whose extension matches no rule (top level and nested, same skips).
  - `func ApplyTidy(root string, moves []Move) (done []Move, skipped []string, err error)`
    - Re-checks size and mtime. On a clash, uses the ` (n)` suffix. Uses `git mv` for git-tracked files (`git ls-files --error-unmatch`), otherwise `os.Rename`.
    - Writes `tidy-last.json` beside `project.yaml`, and an Audit line per move.
  - `func UndoTidy(root, projectDir string) (done, skipped []string, err error)`: the reverse of `tidy-last.json`. Skips files changed since the move. Deletes `tidy-last.json` when finished.
  - HTTP:
    - `GET /api/files/tidy?project=` → `Plan`. When the state is not `OK`: 409 `{state}`.
    - `POST /api/files/tidy {project, moves:[Move]}` → `{done, skipped}`. The server accepts only moves that the current `PlanTidy` also contains: client paths are never trusted.
    - `POST /api/files/tidy/undo {project}`.

- [ ] **Step 1: Failing tests** (`tidy_test.go`):
  - `TestPlanTidy`:
    - Files: `a.fbx`, `Export/b.fbx`, `Export/sub/c.fbx`, `node_modules/x.fbx`, `.cache/y.fbx`, `readme.md`.
    - Expected moves: `a.fbx → Export/a.fbx` only. Homeless: `readme.md`.
  - `TestPlanSkipsIgnored`: in a git repo with `.gitignore` = `build/` → `build/z.fbx` is not planned (skip if git is missing).
  - `TestApplyNoGit`: not a repo → it renames, and `tidy-last.json` exists.
  - `TestApplyGitMv`: a tracked file → `git status --porcelain` shows a rename (skip if git is missing).
  - `TestApplySkipsChanged`: change the file after planning → skipped.
  - `TestApplyClash`: `Export/a.fbx` already exists → `Export/a (2).fbx`.
  - `TestUndo`: apply then undo → the original layout. Change one moved file first → that one is skipped.
  - `TestTidyAPIRejectsForeignMoves` (serve): POST a move not in the plan (`../../etc`) → 400.
- [ ] **Step 2:** Run `go test ./internal/folders/ ./internal/serve/ -run Tidy`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5: Commit** `files: tidy button plan, apply with git mv, undo last round`

### Task 7: Files view in the Console

**Files:**
- Create: `assets/console/js/views/files.js`
- Modify: `assets/console/js/app.js` (route `files`, nav under the project's work section next to Wiki), `assets/console/css/console.css` (appended rules), i18n, `docs/files.md` (new: how folder rules, upload and tidy work), `CHANGELOG.md`, `docs/DESIGN.md` (row)

**Interfaces:**
- Consumes:
  - `api.listFiles`, `api.uploadFiles` (Task 5)
  - `api.tidyPlan(project)`, `api.tidyApply(project, moves)`, `api.tidyUndo(project)` (add to `live.js`, mapping to Task 6)
  - `api.folderRules(project)` → `GET /api/files/rules?project=` returns `{state, rows, bad}` (add this tiny route to `tidyapi.go`)

- [ ] **Step 1:** Add `GET /api/files/rules` with test `TestRulesRouteStates` (none / pending / ok / changed). Run → FAIL → implement → PASS.
- [ ] **Step 2:** Build the view. Contents:
  - **Rule cards:** one per rule folder (icon, name, what, extension chips, file count), plus a "root" card.
  - **State banner:**
    - `none`: "The AI will ask how to split folders the first time it saves a file".
    - `pending`: a link to the draft.
    - `changed`: a caution banner.
  - **List:** a breadcrumb list of the current folder.
  - **Drop zone:** the whole view takes drops, plus an Upload button with progress.
  - **Tidy button:** opens a drawer with grouped moves (checkboxes, all on), a "No place yet" group (read-only), and Confirm. Then a toast with Undo, which also stays as a button while `tidy-last.json` exists (the plan response carries `canUndo`).
  - Follow the existing visual language (cards, `lamp`, `data-tip`). Less text, more icons and counts, per the earlier UI direction.
- [ ] **Step 3:** Verify in the browser preview with `HPK_CONSOLE_DIR`: drop 3 files → each lands in its rule folder. Tidy a misplaced file → moved. Undo → back. Run `node --test tools/*.test.mjs` → PASS.
- [ ] **Step 4:** Write `docs/files.md`, and add CHANGELOG and DESIGN rows.
- [ ] **Step 5: Commit** `console: Files view with rule folders, upload and tidy`

### Task 8: hooks: ask once, show rules, warn on misplaced writes

**Files:**
- Modify: `internal/hook/hook.go` (`sessionLink` returns context; `boardHeartbeat` path or a new branch in the `post_tool_call` dispatch), `assets/skills/hpk-wiki/SKILL.md` (section "Folder layout")
- Test: `internal/hook/folders_test.go` (new), plus the existing latency test

**Interfaces:**
- Consumes: `folders.Load`, `Rules.For`, `project.LinkOf` / `privacy.ResolveSession`.
- Produces:
  - `pre_llm_call`: `Reply{Context: …}` once per session per state change.
    - The state+hash is kept in `<home>/hpk/sessions/<id>.folders`. Write it only when it changes.
    - Text by state:
      - `none`: "Before saving the first file into this project, ask the person which folders to use and what goes in each, then draft `decisions/folders.md` with the table `| Folder | What goes here | Extensions |` (hpk wiki draft). If they want no split, draft the page with the header only."
      - `pending`: "Folder rules are waiting for approval (draft #n). Do not ask again."
      - `ok`: "Project folders: `RAW 3D` (.blend .max), …" (one line).
      - `changed`: "The folder rules page was changed outside approval; ask the person to approve it again. Save files in the project root until then."
  - `post_tool_call`: for `write_file`, `patch` and any tool whose `tool_input` has `path` or `file_path`.
    - If the file is under the project root, `Rules.For` gives a folder, and the file is not under it → `Reply{Context: "<file> belongs in <folder>/ by the project's folder rules"}`.
    - Never blocks.

- [ ] **Step 1: Failing tests**:
  - `TestFolderContextOncePerState`: first call → context with "ask the person". Second call → no context. After approval → context lists the folders.
  - `TestMisplacedWriteWarns`: approved rules. `write_file` `scene.fbx` in the root → context mentions `Export/`. `Export/scene.fbx` → none.
  - `TestHookLatency` (existing): still within budget.
- [ ] **Step 2:** Run `go test ./internal/hook/`. Expected: FAIL.
- [ ] **Step 3:** Implement. Keep the folders work lazy: stat the page first, and parse only when the hash changed.
- [ ] **Step 4:** Run `go test ./internal/hook/`. Expected: PASS. Write the SKILL.md section (same rules as the context text, plus the table format).
- [ ] **Step 5: Commit** `hooks: AI asks for the folder layout once, sees it after, gets nudged when it saves elsewhere`

### Task 9: team features greyed out for v1 + ROADMAP

**Files:**
- Create: `docs/ROADMAP.md`, `internal/serve/features.go`, `internal/serve/features_test.go`
- Modify:
  - `internal/serve/devicesapi.go:131,168,213,224`, `internal/serve/teamserverapi.go:141`, `internal/serve/auth.go:370-431` (solo-mode gate)
  - The snapshot handler (add `features`)
  - `assets/console/js/app.js:58-80,165,202`, `assets/console/js/views/wizard.js:71-73`, `assets/console/js/views/projects.js`, `team.js`, `teamserver.js`
  - i18n, `docs/team.md` (note), `README.md` (one line), CHANGELOG

**Interfaces:**
- Produces:
  - `func featureOn(name string) bool`: reads `HPK_FEATURES` (comma list). `team` is off by default.
  - `func (s *Server) teamGate(h http.HandlerFunc) http.HandlerFunc` → solo mode with team off: 404 `{"error":"team features come in a later version"}`.
  - Snapshot JSON gains `"features":{"team":bool}`. Console: `api.feature('team')`.

- [ ] **Step 1: Failing tests** (`features_test.go`), solo server, `HPK_FEATURES` unset:
  - `POST /api/join`, `POST /api/teamserver/join`, `POST /api/team/join-code`, `GET /api/team`, `GET /api/team/devices` → 404 with that message.
  - `POST /api/teamserver/leave` and `GET /api/teamserver` → not 404 (already-joined machines keep working).
  - `HPK_FEATURES=team` → `/api/team` is back.
  - A team-mode server (`--team`) → unaffected.
- [ ] **Step 2:** Run `go test ./internal/serve/ -run Feature`. Expected: FAIL.
- [ ] **Step 3:** Implement the gate on exactly those routes. `/api/teamserver/remote/*` stays open only when a team server is already linked (it already checks the link).
- [ ] **Step 4:** Console:
  - **Nav:** `team` and `teamserver` are shown with class `.nav-off` (grey, `aria-disabled`, `data-tip` = `t('features.later')`). Clicking does nothing. Exception: `teamserver` stays live when already joined.
  - **Direct routes:** `#/team` and `#/teamserver` render a small "comes in a later version" panel.
  - **Wizard:** the "team" choice is disabled and grey, and the default kind is `personal`.
  - **Projects list:** the team section is hidden when it is empty.
  - i18n en: "Comes in a later version"; th: "มาในเวอร์ชันถัดไป".
- [ ] **Step 5:** Write `docs/ROADMAP.md`:
  - **Connected mode**, linking `docs/plans/connected-mode.md` §10 steps 1–6: project link, outbox + `hpk sync`, reports and wiki drafts up to the team, team rules down, `team_approval`, team AI account relay, Console for both sides.
  - **Git sync** G0–G5, linking `docs/plans/git-sync.md` §11.
  - OIDC/passkeys.
  - `hpk isolate setup` on macOS/Linux.
  - "Turn `features.team` back on" as the last line.
  - Also: README one line, a `docs/team.md` note at the top, a CHANGELOG entry.
- [ ] **Step 6:** Run `go test ./internal/serve/` and `node --test tools/*.test.mjs` → PASS. Browser preview: the nav shows grey Team items with a tooltip, and the wizard team option is disabled.
- [ ] **Step 7: Commit** `v1 is solo-first: team features greyed out; ROADMAP for the team work`

### Task 10: whole-branch check

- [ ] Run `go vet ./...`, `go test ./...`, `node --test tools/*.test.mjs`, and `GOOS=linux go vet ./...`, `GOOS=darwin go vet ./...`. Expected: all pass. The real-browser test is allowed to run.
- [ ] Rebuild `dist/hpk.exe`. On the real machine, in a normal PowerShell (not the in-app terminal: MSIX virtualisation), run `hpk install --yes` and start hpk. Then, in a test project, chat: "save a cube as fbx" → the AI asks for folders → the draft appears → approve → the folders are created → the AI saves into `Export/`.
- [ ] Note in the PR body anything that could not be checked.
