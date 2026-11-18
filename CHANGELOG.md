# Changelog

## 0.1.0 (unreleased)

First public release. hpk is an independent add-on for Hermes Agent, tested
against Hermes v0.23.1.

**Install**
- Double-click `hpk.exe` (or run `hpk` from outside its install folder): hpk
  installs itself for your user (`%LOCALAPPDATA%\hpk\bin` on Windows, in hpk's
  data folder elsewhere), adds that folder to PATH, adds a Start menu shortcut
  "HPK Console" on Windows, and starts the Console. `hpk self-install` and
  `hpk self-uninstall` do it explicitly; `HPK_NO_SELFINSTALL=1` turns the
  automatic part off.
- One-line installers `install.ps1` and `install.sh` on every release: they
  check the SHA-256 against `checksums.txt` and stop on a mismatch;
  `HPK_VERSION` pins a version. Re-running them updates hpk.
- Release image `ghcr.io/hellooosaksit/hpk-hermes` and
  `docker/compose.release.yaml`: Hermes Agent and hpk in one container, with
  the Console on http://127.0.0.1:7878.
- Beginner install guide in English and Thai (docs/install.md, docs/install.th.md).

**One program, any OS**
- `hpk` with no arguments starts HPK Console once per machine (the port is
  the lock; a run file lets `hpk stop` end it with its built-in browser) and
  opens it in the default browser. Ctrl+C stops it too.
- HPK Console installs as a web app (manifest, icons, a service worker that
  never caches `/api/*`), with a clear "hpk is not running" page when hpk is
  stopped. "Install as app" in Settings, or per-browser steps.
- Desktop notices through the browser (approvals, red lines, a chat that
  finished, the built-in browser waiting for you), opt-in, with quiet hours.
- First-run setup in the Console: installs the tested Hermes Agent v0.23.1
  into hpk's own folder (pinned uv with SHA-256, portable Python) only after
  you agree to the size and sources shown, then adds hpk to Hermes.
  `hpk setup hermes` does the same from a terminal.
- Start at login, per user: Windows Task Scheduler, macOS launchd, Linux
  systemd user unit (`hpk autostart on|off` or Settings).

**Projects and the wiki**
- A wiki per project that grows from finished work: drafts with sources, a
  person approves, git history. `hpk wiki` and the hpk-wiki skill.
- Add an existing project (`hpk project add`): a scan that costs 0 tokens and
  never opens secret files, a cost estimate, consent before anything is read,
  then reading one source at a time under a token budget.
- Privacy modes `shared`, `private` and `isolated`, and
  `model_policy: local_only`, all enforced by hooks.

**Work and decisions**
- One main chat per goal that splits into sub-tasks, each in its own git
  worktree, reporting back.
- Manual, Ask and Auto modes. Red lines (push, delete, secrets, control files,
  model change, egress, …) need a person in every mode.
- A board of claims so two people or agents never take the same work
  unknowingly.
- Scheduled jobs through Hermes cron: they run only when something changed, are
  stopped mid-run at their token budget, and pause at the daily cap.

**HPK Console**
- A Git screen on the project's repository: the history as a graph, and
  fetch, pull, push, branch, merge, rebase, interactive rebase, cherry-pick,
  revert, reset, stash, conflicts and draft PRs. Every command shows the git it
  runs, works in a throw-away worktree instead of your checkout, saves an undo
  point before rewriting history, and asks a person at red lines. Advice on
  when to rebase, merge, push or tidy comes from fixed rules (no AI, 0 tokens).
- A web UI served by `hpk serve`, on real data: chats, approvals, board, wiki,
  red lines, privacy, jobs, accounts, settings. Owners add API keys and reorder,
  reset or remove accounts from the Console; hpk passes keys to `hermes auth`
  on stdin and never stores, logs or shows them.
- Real tokens and cost per session, money caps per day and month, and notices
  through `hermes send` for shared projects.
- MCP connections: ready-made settings for Blender, Roblox Studio, Godot,
  Figma, Playwright, a folder, Context7, DeepWiki and GitHub, or your own.
  Added, tested and removed through `hermes mcp`; tokens go to Hermes' .env
  only. A team server connects web MCPs only, never local programs.
- What each MCP is used by: calls per day, projects and repositories, tools,
  chats and skills, recorded by hpk's hook (names only, never inputs).
- Background jobs: Hermes' background commands and local MCP programs that run
  now, with a Stop that only hits the process that was listed.
- Media in the chat (docs/chat-media.md): files the agent names are shown in
  place. 3D (glTF/GLB, FBX, OBJ+MTL, STL, PLY, DAE, 3MF) with textures,
  animation clips, model and studio lights, wireframe, stats and screenshots,
  at a quality that starts at Low and can be made the default; images with
  zoom, video, audio, PDF, CSV/JSON tables, and charts from ```chart blocks or
  tables. Files come read only from the project folder (`/api/files`).
  three.js 0.187 (MIT) is bundled for the 3D viewer.
- Switch a chat's model without losing its memory (docs/model-switch.md): the
  brief and recalled wiki pages always go along; the whole history goes when it
  fills at most 60% of the new window, else Hermes compacts it first. Another
  provider is a red line; local-only projects stay local. The picker shows a
  context ring, the first-turn cost and the red line per model; the chat marks
  where the model changed, with a way back. *Ask once* puts one question to
  another model in a sub-task and leaves the chat's model and cache alone.
- Appearance: theme, accent colour, background, text size, spacing, corners,
  font and motion, per browser. A bundled font (Anuphan, SIL OFL), custom
  pickers instead of the browser's, and help text behind (i) tooltips.
- English and Thai.

**Built-in browser and vault** (docs/browser.md)
- The agent gets a browser on your own machine (`hpk mcp-browser`, headless
  Chrome/Edge driven by hpk). Watch it live in a panel docked on the right or
  along the top; take over with one click; every step is recorded with a
  screenshot, private to you, kept 30 days unless pinned.
- A vault for passwords, logins, tokens and `.env` values, encrypted with a key
  your OS protects. The AI sees only `{{secret:name}}`: hpk types the value,
  only on the sites the secret allows, and scrubs it from everything the AI
  reads. Changes, deletes, showing a value and writing `.env` need a PIN typed
  in the Console. Agent tools that name the vault, a managed `.env`, the
  browser profile or recordings are refused.
- Off on a team server.

**Security pass** (2026-11-18 review)
- Solo mode answers only loopback host names (stops DNS rebinding), and every
  change needs the **Console key**: open the Console with `hpk open` (or the
  one-time link `hpk serve` prints); other programs on the machine can read but
  not change anything.
- Team mode: chats can be read or written only by their owner, people they are
  shared with, or a Console owner; approvals stay in their project; rotating a
  token or removing a member ends their sign-ins and event streams; the device
  token is sent only to the server it was issued by; the local proxy no longer
  creates invites or changes roles; join attempts are counted up front;
  `X-Forwarded-Proto` is trusted only from `trusted_proxies`.
- Vault and browser: a PIN before the first secret, sealed with the secrets;
  exact-host sites (`*.example.com` to include subdomains), https only; a
  password goes only into a password field; fills happen in one step inside the
  page; wider scrubbing; no text questions on a page holding a filled secret;
  hpk never attaches to a browser it did not start.
- Files, gates, processes, MCP, git: `/api/files` checks the resolved path and
  Windows name forms; red lines see through `git -C`, subshells and more
  egress forms, and cover hpk's own files and MCP tools; background jobs are
  matched by start time and Hermes ancestry before Stop; team servers refuse
  private and metadata addresses for MCPs; git refuses refs starting with `-`.
- **The AI under its own OS identity** (`hpk isolate setup|status|check|teardown`,
  docs/isolation.md): Hermes runs as a separate standard account `hpk-ai`
  (Windows, started with CreateProcessWithLogonW through `hpk ai-relay`) or in
  the docker/hermes container (`--docker`). The vault and its key, the browser
  profile and recordings and the Console key move to a private folder only
  your account can open; the AI shares only the Hermes home and the project
  folders. Every step is shown first and confirmed in a terminal; the account
  is made through Windows' own administrator prompt. `hpk isolate check`
  verifies from the AI's side.
- The built-in browser's DevTools is a **pipe** (`--remote-debugging-pipe`;
  handle numbers on Windows), no longer a TCP port; the port is only a
  fallback while the AI is not isolated.
- `hpk doctor` warns while the AI runs in your account. Agent tool calls that
  name the private folder or `hpk isolate` are refused.
- Remaining limits are listed in docs/browser.md (Limits) and docs/isolation.md.

**Teams**
- Team mode: owner, maintainer, member and viewer roles; sign-in with personal
  tokens or through a reverse proxy; TLS for serving other machines.
- Money caps per person, chats shared with chosen members, and a personal
  shared wiki next to the team's.

**Docker**
- An image for a team server: non-root, read-only root filesystem, health
  check, owner token written to the volume, never to the log. A compose file
  for running behind a TLS proxy.
- An image with Hermes Agent and hpk together, built on the official Hermes
  image: hpk installed into Hermes and its hooks allowed on start, HPK Console
  as a service next to the gateway.

**Install**
- `hpk install` adds hooks and skills to Hermes' `config.yaml` as a diff you
  approve, with a backup. `hpk uninstall` restores the file. `hpk doctor` and
  `hpk selftest` check the setup.

See docs/ for each part, and docs/DESIGN.md (Thai) for the full design.
