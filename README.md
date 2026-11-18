🇹🇭 [คู่มือภาษาไทย](docs/install.th.md)

# Hermes Project Kit (hpk)

> Working name. An independent add-on for [Hermes Agent](https://github.com/NousResearch/hermes-agent) — not an official Nous Research product.

hpk makes a project's agent work visible and governable:

- **One main chat per goal.** The agent splits the work into sub-tasks; each runs in its own git worktree and reports back with evidence ("reported done" is never "verified").
- **Humans decide at named points.** Manual / Ask / Auto modes, and red lines (push, delete, secrets, model change, …) that no mode can cross.
- **A per-project wiki** that grows from finished work, with sources, approval and an audit trail. Confidential projects can opt out of the shared wiki.
- **A board of claims** so two people (or two agents) never do the same work without knowing.
- **HPK Console**, a web UI served from the same binary.

Status: **M9** — team mode: people, roles (owner, maintainer, member, viewer), sign-in with personal tokens or through a reverse proxy, and a Console that serves other machines only with sign-in and TLS ([docs/team.md](docs/team.md)), and a Docker image for a team server, tested behind a TLS proxy ([docs/docker.md](docs/docker.md)). Tested for real against Hermes v0.23.1: reading a project into its wiki from a Console chat, red-line approvals, and scheduled jobs that run, skip when nothing changed, and stop mid-run at their budget. hpk counts real tokens and cost per session, enforces money caps, and can send notices through `hermes send` for shared projects ([docs/budgets.md](docs/budgets.md)). `model_policy: local_only` is enforced ([docs/privacy.md](docs/privacy.md)). MCP connections (web MCPs everywhere, local programs such as Blender or Roblox Studio only on your own machine), what uses each MCP, and background jobs you can stop ([docs/mcp.md](docs/mcp.md)). The chat shows what the agent makes: 3D models with textures and animation (FBX, glTF, OBJ, …), images, video, PDF, tables and charts ([docs/chat-media.md](docs/chat-media.md)). Earlier: M8 install, doctor and selftest ([docs/install.md](docs/install.md)), M7 Console on real data, M6 cron, M5.5 board, M5 sessions, M4 gates, M3 add-project, M2 privacy, M1 wiki core, M0 skeleton. See [docs/DESIGN.md](docs/DESIGN.md) (Thai) for the full design and [docs/m0-findings.md](docs/m0-findings.md) for what we verified against Hermes.
- [Built-in browser and vault](docs/browser.md): the AI browses on your machine and uses your passwords without seeing them.
- [Run the AI under its own OS account](docs/isolation.md): `hpk isolate setup` keeps the vault, the browser and the Console key out of the AI's reach (Windows: a separate local account; or a container).

## Install

hpk is one program, and HPK Console is a web app, so it works the same on
Windows, macOS and Linux. Pick one way:

**Windows.** Download `hpk.exe` from the
[Releases page](https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest)
and double-click it (if SmartScreen warns: More info, then Run anyway). Or, in
PowerShell:

```powershell
irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.ps1 | iex
```

**macOS / Linux:**

```sh
curl -fsSL https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.sh | sh
```

**Docker** (Hermes Agent and hpk together). In an empty folder, download the
compose file (macOS/Linux; on Windows PowerShell use
`irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/compose.release.yaml -OutFile compose.release.yaml`),
change `HPK_OWNER` in it to your email, then run these in that folder:

```sh
curl -fsSLO https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/compose.release.yaml
docker compose -f compose.release.yaml up -d
docker compose -f compose.release.yaml exec hermes cat /opt/data/hpk/owner-token
# then open http://127.0.0.1:7878 and paste the token
```

hpk installs itself for your user (no administrator rights), adds itself to
PATH and starts HPK Console on http://127.0.0.1:7878 in your browser (once per
machine: a second `hpk` just opens it). Stop it with Ctrl+C or `hpk stop`;
remove it with `hpk self-uninstall`. Update by running the same install again.

First run: the Console checks for Hermes Agent. If it is missing, it offers
to install the tested version (v0.23.1) into hpk's own folder with its own
portable Python, showing the size and every source first. Then it adds hpk
to Hermes (the same change `hpk install` shows) after you agree. Then add a
model account in Settings, AI accounts.

Optional, in Settings › General: **Install as app** (its own window and
icon, from Chrome, Edge or Safari), desktop notices with quiet hours, and
**Start hpk when I log in**.

Step by step for beginners: [docs/install.md](docs/install.md) (Thai:
[docs/install.th.md](docs/install.th.md)).

## Build

Requires Go 1.27+.

```sh
go test ./...
go build -o dist/hpk ./cmd/hpk
```

## Use

```sh
hpk                   # start HPK Console once per machine and open it (--no-open: just start)
hpk stop              # stop it, with its built-in browser
hpk setup hermes      # install the tested Hermes Agent into hpk's own folder; --yes after reading the plan
hpk autostart on      # start hpk serve when you log in (per user); off | status
hpk install           # shows the change to Hermes' config.yaml; --yes applies it (docs/install.md)
hpk doctor            # checks Hermes, hooks, skills, projects, serve
hpk selftest          # core flows in a throwaway folder; run after each Hermes update
hpk serve             # HPK Console on http://127.0.0.1:7878 (loopback only), on your real projects
hpk hook <name>       # called by Hermes hooks: JSON event on stdin, JSON decision on stdout
hpk wiki init         # set up a project wiki (folders, SCHEMA, index, log, its own git history)
hpk wiki draft …      # propose a page; sources required, secrets refused
hpk wiki approve 1,3  # a person approves; Auto may approve lessons/versions/glossary only
hpk wiki lint         # missing sources, oversize pages, rule-like lines, broken index
hpk wiki search …     # index first, then pages; no embeddings
hpk project new <name> --root DIR --privacy private   # shared | private | isolated
hpk project privacy <name> shared --yes               # opening up needs a person
hpk wiki promote <page> --project NAME                # redacted draft in the shared wiki; a person approves
hpk project add <dir>                                 # asks first: depth, privacy, estimated cost; secrets never opened
hpk project ingest <name> --next                      # Hermes reads one queued source at a time, under a token budget
hpk project health <name>                             # broken links, missing files, repeated rules, undocumented folders
hpk gates mode <project> auto --for 2h                # Manual | Ask | Auto; red lines ask a person in every mode
hpk gates check --cmd "rm -rf x && git push"          # dry run: which red line each part crosses
hpk gates digest <project>                            # what Auto let through today, numbered
hpk session new --project P --parent 1 --name "auth › tests"  # sub-task: own worktree, brief, lineage number
hpk session report 1.1 --project P < report.txt       # ≤15 lines back to the main chat (Done/Decided/Lessons/Open/Question)
hpk acp probe                                          # ACP handshake with hermes-acp, no model call
hpk board claim --task T --path "api/invoices/**"       # 🔴 same task stops · 🟠 same files asks · 🟡 related tells
hpk board who api/invoices/totals.ts                  # who holds it, from the board, nobody disturbed
hpk board render                                      # BOARD.md from board.db
hpk cron install --project P                          # shows the 3 Hermes cron jobs; --yes creates them
hpk cron status --project P                           # pauses, locks, tokens in 24 h / 7 days
hpk cron uninstall --project P                        # shows what it removes from Hermes cron; --yes removes
hpk wiki raw sessions/<date>-<topic>.md < notes.md     # source material under raw/; hpk writes it, not the agent
hpk team init --owner you@example.com                  # team mode; prints your token once (docs/team.md)
hpk team add max@example.com --role member            # owner | maintainer | member | viewer
hpk version --json
```

Every command that prints data takes `--json`.

## Design rules that keep this safe

- hpk talks to Hermes only through its official interfaces: the `hermes` CLI, ACP (`hermes-acp`), hooks and skills. It never patches Hermes, and it does not use or copy code from any Hermes web UI.
- hpk does not call LLM APIs itself; model work goes through Hermes, so your keys, budgets and caches stay there.
- Guards that protect you (red lines, privacy) fail closed: if they cannot decide, they deny.
- `hpk serve` listens on loopback only, unless team mode is on (sign-in for every request) and TLS is set up.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Report security problems privately: [SECURITY.md](SECURITY.md). Changes: [CHANGELOG.md](CHANGELOG.md).

## License

**Source-available, not open source.** hpk is released under the MIT license with the [Commons Clause](https://commonsclause.com/) condition — see [LICENSE](LICENSE).

In plain words: you may use, change and share hpk, including inside your company. You may **not sell** hpk, or a product or service whose value comes entirely or substantially from hpk (including paid hosting or support of it).

This summary is not legal advice; the LICENSE file is what applies.
