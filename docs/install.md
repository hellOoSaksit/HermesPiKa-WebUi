ภาษาไทย: [install.th.md](install.th.md)

# Installing hpk

hpk adds itself to Hermes Agent in one place: the profile's `config.yaml`. It
registers six hooks and one read-only skills folder. It never edits any other
Hermes file, it never reads `.env`, and it writes only after you say yes.

## Quick install

This part is for first-time users. It takes about five minutes. The longer
sections below it explain the details.

### 1. What you need

- A computer with Windows 10 or 11, macOS, or Linux.
- An internet connection.
- A web browser (Chrome, Edge, Firefox or Safari).
- About 1 GB of free disk space, if hpk also has to install Hermes Agent.
- An account with an AI provider (for example an API key or a login). Hermes
  Agent uses it to talk to a model. You add it after the first start.

You do not need administrator rights. hpk installs for your user only.

### 2. Windows (Docker, one command)

On Windows, hpk runs in Docker: Hermes Agent and hpk together in one container. Nothing is installed on Windows itself, so antivirus has no program to flag.

1. Install [Docker Desktop](https://www.docker.com/products/docker-desktop/) (free for personal use and small businesses), start it, and wait until it says it is running.
2. Open PowerShell and run:

```powershell
irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.ps1 | iex
```

3. The script downloads `compose.release.yaml`, checks it against the published SHA-256 checksum, saves it with your settings in `%USERPROFILE%\hpk`, and starts the container. The first time this takes a few minutes.
4. It opens HPK Console in your browser with a one-time link. That signs this browser in for 30 days. hpk runs in solo mode (one person, this computer only), so there is no account or password. Next time, open it from the Start menu: **HPK Console**. The shortcut starts hpk if needed and opens a fresh link, so you never type a command.

Your `Documents` folder is the Console's `/projects` folder. To add a project in `Documents\my-app`, enter `/projects/my-app`. To use another folder, set `HPK_PROJECTS` (for example `$env:HPK_PROJECTS='D:\work'`) before you run the script the first time, or change `HPK_PROJECTS` in `%USERPROFILE%\hpk\.env` and run `docker compose -f compose.release.yaml up -d` in that folder. The container starts again by itself whenever Docker Desktop runs.

### 3. macOS / Linux (one command)

Open a terminal and run:

```sh
curl -fsSL https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.sh | sh
```

The script downloads hpk, checks it against the published SHA-256 checksum
(a fingerprint of the file), and stops if the fingerprint does not match. Then
it installs hpk for your user and starts HPK Console.

- To install a specific version, set `HPK_VERSION` (for example `HPK_VERSION=0.1.0`).
- To install without starting HPK Console, set `HPK_NO_START=1`.
- On macOS the script removes the "downloaded from the internet" mark, so Gatekeeper does not block hpk.
- On macOS and Linux, hpk adds its folders (its own and the Hermes one) to PATH with one marked line in your shell profile files (`~/.profile`, `~/.zshrc`, `~/.bashrc`, `~/.bash_profile`): in each one that already exists. If none exists (a new Mac account, for example), hpk creates the one for your shell (`~/.zshrc` for zsh, `~/.bash_profile` for bash on macOS, `~/.bashrc` for bash on Linux, otherwise `~/.profile`). It tells you which file it changed.
- At a terminal, the script starts HPK Console in the foreground (Ctrl+C stops it). Without a terminal it starts it in the background and prints the address and a log file.

You can also run `hpk self-install` yourself at any time. Set
`HPK_NO_SELFINSTALL=1` if you do not want hpk to install itself automatically.

### 4. Docker

Use this if you prefer to run Hermes Agent and hpk together in a container. You need Docker with Compose.

1. Make an empty folder and open a terminal in it. Run every command below in that folder.
2. Download the compose file. On macOS or Linux:

```sh
curl -fsSLO https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/compose.release.yaml
```

On Windows (PowerShell):

```powershell
irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/compose.release.yaml -OutFile compose.release.yaml
```

3. Make a file `.env` next to it with one line, `HPK_PROJECTS=<the folder that holds your projects>`. The Console sees that folder as `/projects`. Without it, a `projects` folder next to the compose file is used.
4. Start it:

```sh
docker compose -f compose.release.yaml up -d
```

5. Wait a few seconds for it to start, then print the one-time link. If the file is missing, wait a little and try again:

```sh
docker compose -f compose.release.yaml exec hermes cat /opt/data/hpk/console-link
```

6. Open that link in your browser. It works once, within 10 minutes, and signs that browser in for 30 days. For another browser, print a new link.
7. Add a model account inside the container:

```sh
docker compose -f compose.release.yaml exec -u hermes hermes hermes auth add <provider>
```

If you use Git Bash on Windows, put `MSYS_NO_PATHCONV=1` in front of the `docker` commands. More in [docker.md](docker.md).

### 5. First run: Hermes is found or installed, then add a model account

1. HPK Console looks for Hermes Agent on your computer.
2. If it finds Hermes, it uses it.
3. If it does not, it offers to install the tested version, Hermes v0.23.1, into hpk's own folder. It first shows the download size and every source. Nothing is downloaded until you tick "I agree" and press Install.
4. Then it shows the one change it wants to make to Hermes (adding hpk). Press "Add hpk to Hermes" to accept.
5. Last, add a model account. Open a **new** terminal (so it can find `hermes`), then run:

```sh
hermes auth add <provider>
```

   On macOS and Linux you can also do it in the Console: **Settings**, then **AI accounts**, has a form to add a key. On Windows that page shows this command. Without an account, the agent cannot answer.

### 6. Update

Run the same install again (step 2 or 3). It stops a running hpk first, then replaces the old copy. Your data and projects stay as they are.

### 7. Uninstall

```sh
hpk self-uninstall
```

First, if you added hpk to Hermes, take it out again:

```sh
hpk uninstall --yes
```

Then `hpk self-uninstall` removes hpk, its PATH entries, the Start menu shortcut and start at login. It leaves your hpk data and your Hermes home alone, so you lose nothing. The folder of the Hermes that hpk installed leaves your PATH too, so the `hermes` command will no longer be found in a new terminal (the files are still in the data folder). To delete everything, remove the data folder yourself: `%LOCALAPPDATA%\hpk` on Windows, `~/Library/Application Support/hpk` on macOS, `~/.local/share/hpk` on Linux (`$XDG_DATA_HOME/hpk` if you set it).

### 8. Troubleshooting

- **"Port 7878 is in use".** Another program, or another hpk, is using the port. Run `hpk stop`, or start on another port: `hpk --addr 127.0.0.1:7879`.
- **`hpk` is not found after installing.** PATH changes only reach terminals opened afterwards. Close the terminal and open a new one.
- **macOS says the app cannot be opened (Gatekeeper).** Use the one-command install (step 3), which removes the mark. Or run `xattr -d com.apple.quarantine <path to hpk>`.
- **Windows: "Docker is not installed" or "not running".** Install or start Docker Desktop, wait until it says it is running, then run the install command again.
- **Over SSH (no browser on that machine).** Forward the port, then open the address on your own computer: `ssh -L 7878:127.0.0.1:7878 you@host`, then open http://127.0.0.1:7878 locally.
- **Something else.** Run `hpk doctor`. It checks Hermes, hooks, projects and the Console.

---

## The short way: `hpk`

Run `hpk` with no arguments. It starts HPK Console (the same daemon as
`hpk serve`) on http://127.0.0.1:7878 and opens it in your default browser.

- **Once per machine.** The listening port is the lock: if hpk already runs
  there, `hpk` only opens the Console; if two start at the same moment, the
  second waits for the first. Another program on the port is reported, not
  replaced (`hpk --addr 127.0.0.1:<port>` picks another).
- **Stopping.** Ctrl+C in that terminal, or `hpk stop` from anywhere. While it
  runs, hpk keeps a run file (`<hpk data>/run/serve-<port>.json`, readable only
  by you) with a random stop key; `/api/shutdown` accepts only that key, only
  from this machine. Stopping also closes the built-in browser and the chats
  hpk holds open.
- **The Console key.** `hpk` opens the one-time link that `hpk serve` writes to
  `<hermes home>/hpk/console-link` (`http://127.0.0.1:7878/k/…`, single use,
  10 minutes), so that browser may make changes; it falls back to the bare
  URL only when there is no link. Another browser or tab can look but not
  change anything until you run `hpk open`. The service worker never caches
  `/k/*`, and a page that hpk refuses for want of the key shows "run
  `hpk open`" instead of the Console.
- **`--no-open`** starts without opening a browser.

hpk's own data folder: `%LOCALAPPDATA%\hpk` on Windows,
`~/Library/Application Support/hpk` on macOS, `$XDG_DATA_HOME/hpk` (or
`~/.local/share/hpk`) on Linux; `HPK_DATA_DIR` overrides it.

### First run in the Console

The add-project page first checks two things:

1. **Hermes Agent.** If `hermes` is not on PATH, the Console offers to install
   the tested release, **v0.23.1**, into `<hpk data>/hermes`, and shows every
   source and an estimated size (about 255 MB to download, 820 MB on disk)
   before anything happens. Nothing is downloaded until you tick "I agree" and
   press Install. Steps:
   - uv 0.13.5 from Astral's GitHub release, checked against the SHA-256 that
     Hermes v0.23.1 itself pins in `pm/lock.json` (a mismatch stops the install);
   - Python 3.14, portable (python-build-standalone), downloaded and checked by
     uv into `<hpk data>/hermes/python` (`--no-bin`, and on Windows
     `--no-registry`: no system Python is used or changed);
   - Hermes Agent from its `v0.23.1` tag on GitHub with the `acp` extra, by
     `uv tool install`, into `<hpk data>/hermes/tools`, with `hermes` and
     `hermes-acp` in `<hpk data>/hermes/bin`.

   hpk puts that `bin` folder first on its own PATH, so the doctor, chats and
   jobs find it. The installed hpk also puts it on your user PATH, so `hermes` works in a new terminal. To
   remove it, delete `<hpk data>/hermes`. From a terminal: `hpk setup hermes`
   shows the same plan, `hpk setup hermes --yes` runs it. This is hpk's own
   install, not Hermes' official installer (`scripts/install.sh`/`.ps1`, which
   also sets up git, Node and optional tools); if you prefer that one, use it
   and hpk finds `hermes` on PATH.
2. **hpk in Hermes.** The same change as `hpk install` (below), shown with its
   diff, applied after you press "Add hpk to Hermes".

Then connect a model account with `hermes auth add <provider>` in a terminal.

### Hermes Agent's licence (checked for an offline bundle)

Hermes Agent v0.23.1 is under the **MIT licence** (`LICENSE`: "Copyright (c)
2025 Nous Research"). MIT allows copying and redistributing it, including in
a bundle, as long as the copyright and permission notice go with every copy.
So an offline bundle is allowed by Hermes' own licence, but it would also have
to carry the licences of everything it pulls in: Python (PSF licence), uv
(MIT or Apache-2.0), and Hermes' Python dependencies, each under its own terms
(and some optional extras download further programs). hpk does **not** ship
an offline bundle today: it downloads from the original sources, pinned and
checked, after the person agrees. A bundle would need a licence inventory of
the resolved dependency set first. hpk itself is MIT + Commons Clause, which
does not change Hermes' MIT terms. (Not legal advice; have someone qualified
review before distributing a bundle.)

### Install as an app

HPK Console is an installable web app (PWA): a manifest, icons and a small
service worker, all served by hpk itself under the same strict
Content-Security-Policy (`default-src 'self'`). It works from
http://127.0.0.1 (browsers treat loopback as a secure context) and from a team
server over HTTPS.

- Chrome, Edge and other Chromium browsers: Settings › General › **Install as
  app**, or the install icon in the address bar.
- Safari 17+ on macOS: File › Add to Dock. iPhone/iPad: Share › Add to Home Screen.
- Firefox on the desktop does not install web apps; use a pinned tab.

The installed app is the same Console in its own window. `hpk` opens the
Console in your default browser; Chromium browsers may hand that link to the
installed app. The service worker caches the fonts, icons and bundled
libraries for a fast start, never caches `/api/*`, and shows "hpk is not
running — start it with `hpk`" when hpk is stopped (the page returns to the
Console by itself once hpk answers).

### Desktop notices

Settings › Notifications › **Desktop notices on this computer**: approvals
waiting, red lines, a chat that finished a turn, and the built-in browser
waiting for you. They use the browser's Notifications API, so they work on any
OS the browser runs on; they are off until you turn them on (the browser asks
once), are kept per browser, and can skip times when the Console is in front
and quiet hours (for example 22:00–07:00). The Console's bell keeps every
notice either way.

### Start at login (the OS-specific part)

Settings › General › **Start hpk when I log in**, or `hpk autostart on|off|status`.
It runs `hpk serve` in the background for you only (no administrator rights,
no window, no browser):

| OS | How |
|---|---|
| Windows | a Task Scheduler task `hpk` with a logon trigger for your account, run through `conhost --headless` (definition in `<hpk data>/autostart/hpk-task.xml`) |
| macOS | `~/Library/LaunchAgents/local.hpk.serve.plist`, loaded with `launchctl bootstrap gui/<uid>`; log in `<hpk data>/logs/serve.log` |
| Linux | `~/.config/systemd/user/hpk.service`, `systemctl --user enable` (starts at your next login) |

It runs the hpk binary that was running when you turned it on; turn it off and
on again after moving hpk. This is the only part of hpk that differs per OS.

## Requirements

- Hermes Agent v0.23 or newer, with `hermes` on PATH, or the one `hpk setup hermes` installs (`hermes-acp` is needed to start chats from the Console)
- git

## 1. See what would change

```bash
hpk install
```

This prints every change, plus a line diff of `config.yaml`, and then exits
without writing anything (exit code 3, "needs consent"). For a named Hermes
profile, add `--profile <name>`. To work on another Hermes home, add
`--home <dir>`.

## 2. Install

```bash
hpk install --yes
```

This does the following, in order:

1. Copies `config.yaml` to `config.yaml.hpk-backup-<time>` next to it.
2. Copies the running hpk binary to `<hermes home>/hpk/bin/`. The hooks call this copy, so moving or deleting your download later does not break them.
3. Copies hpk's skills to `<hermes home>/hpk/skills/` and adds that folder to `skills.external_dirs`. Hermes treats external skill folders as read-only.
4. Adds hpk's hooks under `hooks:`:

| Event | Hook | If the hook fails |
|---|---|---|
| `pre_tool_call` | `red_line_guard`: a person approves push, delete, secrets, control files, model change, and egress | the call is denied (`fail_closed`) |
| `pre_tool_call` | `privacy_guard`: private and isolated projects keep their content on this machine | the call is denied (`fail_closed`) |
| `pre_tool_call` | `board_guard` (only `write_file` and `patch`): warns before two chats edit the same files | the call goes ahead |
| `post_tool_call` | `board_heartbeat`: keeps claims fresh | — |
| `on_session_end` | `child_report`: files a sub-task's report back to its main chat | — |
| `post_approval_response` | `approval_audit`: records who approved what | — |
| `pre_api_request`, `pre_auxiliary_call` | `model_route`: records where each model request goes, so `privacy_guard` can enforce `model_policy: local_only` | — (`privacy_guard` then refuses local-only projects) |
| `post_api_request`, `post_auxiliary_call` | `model_usage`: real tokens and cost per session, for budgets, money caps and the Console | — |
| `pre_llm_call` | `session_link`: links a cron run to its project and job (from a tag in hpk's prompt) | — |
| `post_compaction` | `compaction_note`: notes in the audit log that Hermes compacted a session | — |

5. Writes `<hermes home>/hpk/install.json`, which records what it did.

The edit is made as text at fixed lines. Comments, blank lines, quoting, line
endings and every key hpk does not own stay byte for byte as they were. If
`skills` or `hooks` are laid out in a way hpk does not recognise, such as inline
`{...}` maps or a list on one line, hpk stops and writes nothing. Add the
entries by hand instead (see below).

Running `hpk install` again changes nothing when everything is already in
place. After an upgrade, it updates hpk's own entries where they are and adds
nothing new.

## 3. Allow the hooks once

The first time Hermes runs a new hook it asks you to allow it. You can:

- run `hermes` in a terminal once and answer the prompt for each hook,
- start it once with `--accept-hooks`, or
- set `hooks_auto_accept: true` in `config.yaml` (hpk never sets this for you).

Hermes remembers each hook with the time its script last changed. When you
install a newer hpk, `hermes hooks doctor` reports "script modified since
approval" for every hpk hook: review the change, then `hermes hooks revoke`
each one and allow it again. `tools/hermes-consent.py --refresh` (run with
Hermes' own Python) does both for hpk's hooks only. Installing the same binary
again does not copy it, so it does not ask again.

Run `hpk install` from the terminal you normally use for Hermes. A program
that runs inside a packaged desktop app can see a private copy of
`%LOCALAPPDATA%`; its install would not reach the Hermes you use.

## 4. Check

```bash
hpk doctor
```

```bash
hpk selftest
```

`doctor` is read-only. It checks Hermes, `hermes-acp`, git, the hooks and
skills in `config.yaml`, the binary copy, hook consent, each project (wiki,
`board.db`, root folder), and whether `hpk serve` is running.

`selftest` runs the core flows in a throwaway folder: a project and its wiki, a
draft that has no source, a red line, a guard fed broken input, a private
project trying to send data out, and a duplicate claim. It never touches your
Hermes home or calls a model. Run it after every Hermes update.

## Uninstall

```bash
hpk uninstall
```

```bash
hpk uninstall --yes
```

The first command shows what would be removed. The second removes only what
hpk added: its hooks, its skills folder, and keys that hpk created and that are
now empty. A backup is kept. When nothing else changed in between, the result
is byte-identical to the file before install. Projects, wikis, board data and
`hpk-shared/` are kept. Delete `<hermes home>/hpk/` yourself if you want them
gone.

## Adding the entries by hand

Use the paths that `hpk install` prints:

```yaml
skills:
  external_dirs:
    - <hermes home>/hpk/skills

hooks:
  pre_tool_call:
    - command: '"<hermes home>/hpk/bin/hpk.exe" hook red_line_guard'
      timeout: 15
      fail_closed: true
    - command: '"<hermes home>/hpk/bin/hpk.exe" hook privacy_guard'
      timeout: 15
      fail_closed: true
    - command: '"<hermes home>/hpk/bin/hpk.exe" hook board_guard'
      matcher: ^(write_file|patch)$
      timeout: 15
  post_tool_call:
    - command: '"<hermes home>/hpk/bin/hpk.exe" hook board_heartbeat'
      timeout: 15
  on_session_end:
    - command: '"<hermes home>/hpk/bin/hpk.exe" hook child_report'
      timeout: 15
  post_approval_response:
    - command: '"<hermes home>/hpk/bin/hpk.exe" hook approval_audit'
      timeout: 15
  pre_api_request:
    - command: '"<hermes home>/hpk/bin/hpk.exe" hook model_route'
      timeout: 15
  pre_auxiliary_call:
    - command: '"<hermes home>/hpk/bin/hpk.exe" hook model_route'
      timeout: 15
```

On macOS and Linux the binary is called `hpk`, not `hpk.exe`.

## Where hpk keeps things

```
<hermes home>/
  config.yaml                 Hermes' file; hpk only adds the entries above
  hpk/
    bin/hpk(.exe)             the copy the hooks run
    skills/                   hpk-wiki, hpk-project (read-only for Hermes)
    install.json
    projects/<name>/          project.yaml, wiki/, board.db, audit.log, sessions
    routes/                   per Hermes session: where requests went, tokens, cost
    sessions/                 which project (and cron job) each Hermes session belongs to
    hooks.log                 what a guard saw when it asked or blocked
    prices.json               model prices, taken from Hermes' models.dev cache
    team.yaml                 team mode only (docs/team.md); token hashes, mode 0600
  hpk-shared/                 the shared wiki (shared projects only)
```

`<hermes home>` is `$HPK_HOME`, then `$HERMES_HOME`, then the OS default:
`%LOCALAPPDATA%\hermes` on Windows and `~/.hermes` elsewhere. When
`HERMES_HOME` points to a named profile (`.../profiles/<name>`), hpk uses the
home above it, so every profile sees the same projects.

## Sessions that run inside the Hermes gateway

When the Hermes gateway is running, `hermes-acp` and cron hand sessions to it.
Their working directory and environment are then the gateway's, not the
project's, so hooks cannot tell the project from `cwd` or `HPK_PROJECT`. hpk
links each session to its project instead: Console sessions when they open,
cron runs from a `[hpk-job <project>/<job>]` tag at the end of hpk's prompt.
A session hpk did not open and whose `cwd` is outside every project is not
governed by a project (red lines still apply).
