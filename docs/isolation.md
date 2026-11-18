# Running the AI under its own OS identity

Hermes (the AI) runs programs: a terminal, Python, whatever a task needs. If it
runs as **your** OS account, every one of those programs can do what you can.
The 2026-11-18 security review found three things that follow from that:

- the vault key is protected by the OS **per account** (Windows DPAPI, macOS
  Keychain, Linux Secret Service), so anything running as you can decrypt the
  vault;
- the built-in browser's DevTools socket was a TCP port on 127.0.0.1, which any
  program on the machine can reach;
- hpk's local API trusts what runs on this machine (the Console key now limits
  changes to a browser hpk opened, but the link and cookie files are yours,
  and so readable by anything running as you).

hpk's hooks refuse agent tool calls that name these files, which stops
accidents and simple attempts. It is not a boundary: a program written to dig
can still read what your account can read. The boundary is a second OS
identity for the AI. `hpk isolate` sets one up.

## What stays where

| Belongs to you (the AI cannot read it) | Shared with the AI (read and write) |
| --- | --- |
| the vault (`vault.bin`) and its key | the Hermes home: config, sessions, hpk's projects, wikis, board, gates |
| the built-in browser: profile, cookies, recordings | your project folders |
| the Console key: the one-time link and the signed-in browsers | `agent.json`: where `hpk serve` is and the agent key (see below) |
| `isolation.json` (these settings) and the AI account's password | |
| hpk's own folder: the run file with the stop key, the Hermes hpk installed | (Hermes runs from it, read-only) |

Your private files move from `<hermes home>/hpk/` to **`<hpk data>/private`**
(Windows: `%LOCALAPPDATA%\hpk\private`), a folder with only you and SYSTEM on its
access list. hpk finds it through `isolation.json` in **your** config folder
(Windows: `%APPDATA%\hpk\isolation.json`), never through anything in the shared
home, where the AI could plant a file that points hpk somewhere else.

### What crosses the boundary

1. **Starting Hermes.** Everything hpk runs of Hermes (`hermes-acp` for Console
   chats, `hermes cron …`, `hermes send`, `hermes auth list`, `hermes mcp`,
   `hermes --version`, session renames) goes through `hpk ai-relay`, which
   starts it as the AI with the same working folder and stdin/stdout/stderr.
   Only `hermes` and `hermes-acp` are allowed; only these variables cross:
   `PATH`, `HERMES_HOME`, `HPK_HOME` (both forced to the shared home),
   `HPK_PROJECT`, `WIKI_PATH`, `HPK_SESSION` and a few terminal settings. Your
   tokens and profile folders do not.
2. **The hooks.** Hermes runs `hpk hook …` as the AI; the hooks read and write
   the shared home (projects, board, gates, audit), as before.
3. **The built-in browser.** The `hpk-browser` MCP (run by Hermes, as the AI)
   reads `<home>/hpk/browser/agent.json` and calls `hpk serve` over loopback
   with the agent key. That key lets it drive the browser the way the agent
   always could (secrets by name, values never shown, recordings for you);
   nothing else. The file sits in the shared home, so the AI's account and you
   can read it, and the hooks still refuse agent tool calls that name it.

Nothing else is shared: no DevTools port (below), no vault, no Console cookie.

## Options per OS

| | Separate local account | Container (Docker) |
| --- | --- | --- |
| **Windows** | **Default.** A standard user `hpk-ai`; hpk starts Hermes as it with the account's password (kept for you only). Automated: `hpk isolate setup`. | `hpk isolate setup --docker`. Paths inside the container differ from yours (`/host/c/…`), so some hook features that compare paths (board claims by path) see less. Works with Docker Desktop. |
| **macOS** | **Default** (a system user and a launchd job, or `sudo -u`). Not automated yet: steps below. | `--docker` works, but hpk's hooks inside are the container's Linux hpk, and Docker Desktop is needed. |
| **Linux** | A system user (`useradd --system`) with a sudoers rule for `hpk ai-exec`. Not automated yet: steps below. | **Default.** `hpk isolate setup --docker`: the Hermes home at `/opt/data`, project folders at their own paths, the container's `hermes` user set to your uid so files stay yours. |

Why these defaults: on Windows a second local account is built in, needs no
extra software, and DPAPI, profile folders and access lists already separate
accounts; Docker Desktop on Windows adds a VM and path translation. On Linux
Docker is common and gives the cleanest separation with identical paths. On
macOS a second account fits launchd and the Keychain, which are per user.

### What `hpk isolate setup` does on Windows

It prints every step first and asks you to type `isolate` in a terminal (not a
pipe, not a flag; hpk's hooks also refuse `hpk isolate` from the AI). Creating
the account opens Windows' own administrator prompt. Stop hpk first (`hpk stop`).

1. Make `%LOCALAPPDATA%\hpk\private`, access only for you and SYSTEM.
2. Create the standard local account `hpk-ai` (or reset its password), hide it
   from the sign-in screen (`HKLM\…\Winlogon\SpecialAccounts\UserList`). The
   password is random, made on your side and kept DPAPI-protected in the
   private folder. It lets one start programs as `hpk-ai`, nothing more.
3. Move `vault.bin` and its key, the browser profile and recordings, and the
   Console sessions into the private folder; delete the old one-time link.
4. Program folders inside the home (`hpk\bin`, Hermes' `bin`, `hermes-agent`,
   `installs`): `hpk-ai` may read and run, not change.
5. The Hermes home and each registered project folder: `hpk-ai` may read and write.
6. Save `isolation.json`, then start hpk's probe **as `hpk-ai`** and check that it
   cannot open the private folder, the vault or its key, and can write the
   home (`hpk isolate check` repeats this any time).

How hpk starts Hermes as `hpk-ai`: `CreateProcessWithLogonW` (the Secondary
Logon service, which also lets a standard user do it) starts
`<home>\hpk\bin\hpk.exe ai-exec <spec>` as `hpk-ai`, suspended, puts it in a job
that ends the whole tree when hpk's relay ends, and passes the relay's
stdin/stdout/stderr. `ai-exec` sets the spec's variables and runs Hermes.

`hpk isolate teardown` undoes it in reverse (files back, access entries
removed, the account deleted unless `--keep-user`; its profile folder stays).
`hpk isolate run -- hermes …` runs any Hermes command as the AI, for example the
gateway: `hpk isolate run -- hermes gateway run`.

### Docker

`hpk isolate setup --docker` writes `<hpk data>/isolate/compose.yaml` for the
`docker/hermes` image (`hpk-hermes:local`, see [docker.md](docker.md)) with
the Hermes home at `/opt/data` and each project folder mounted, no ports
published, and starts it. The relay becomes `docker exec -i -u hermes`, with the
working folder and `WIKI_PATH` mapped through the mounts. The image's first
start runs `hpk install` inside, so Hermes' hooks point at the container's
hpk; after `hpk isolate teardown`, run `hpk install` on your machine again.
The `hpk-browser` MCP reaches your `hpk serve` through `host.docker.internal`
(Docker Desktop); on Linux without Docker Desktop the built-in browser is not
reachable from the container (hpk serve listens on loopback only).

### macOS and Linux by hand (separate account)

```sh
sudo useradd --system --create-home --shell /usr/sbin/nologin hpk-ai     # macOS: sysadminctl -addUser hpk-ai …
sudo setfacl -R -m u:hpk-ai:rwX -m d:u:hpk-ai:rwX ~/.hermes ~/code/app   # the home and each project
sudo setfacl -R -m u:hpk-ai:rX ~/.hermes/hpk/bin                         # run, not change
echo "$USER ALL=(hpk-ai) NOPASSWD: $HOME/.hermes/hpk/bin/hpk ai-exec *" | sudo tee /etc/sudoers.d/hpk-ai
```

Then move the private files (`~/.hermes/hpk/{vault,browser/profile,browser/runs,console-sessions.json}`)
into `~/.local/share/hpk/private` (mode 0700) and write `~/.config/hpk/isolation.json`:

```json
{"mode": "user", "user": "hpk-ai", "home": "/home/you/.hermes",
 "private": "/home/you/.local/share/hpk/private", "bin": "/home/you/.hermes/hpk/bin/hpk"}
```

The relay then runs `sudo -n -u hpk-ai -- <bin> ai-exec <spec>`. Check with
`hpk isolate check`. Automating this is a follow-up.

## The built-in browser: a pipe, not a port

hpk now talks to the browser over `--remote-debugging-pipe`: two anonymous
pipes whose other ends only hpk holds. No program and no other account can
connect. On Linux and macOS the pipes are fd 3 and 4 (`ExtraFiles`); on Windows
the browser takes inheritable handle numbers
(`--remote-debugging-io-pipes=<in>,<out>`), inherited by that one child only.
Tested with Chrome 157 on Windows 11.

If a browser does not answer on the pipe within 15 s, hpk falls back to a
TCP port on 127.0.0.1 **only when the AI is not isolated** (nothing new to
guard then); isolated, it refuses and says why. `HPK_BROWSER_DEVTOOLS=pipe` or
`tcp` forces one. The Console's browser state shows which (`devtools`).

## Checks

- `hpk doctor`: **warn** when the AI runs in your account, **ok** when it is
  isolated and the account (or container) and its password are there, **fail**
  when the setup is broken, and a warning for private files left in the home.
- `hpk isolate status`: the settings and the same checks.
- `hpk isolate check`: starts hpk as the AI and tests what it can open.

## Limits

- **Integrity of the shared home.** The AI can still write the Hermes home:
  hpk's gates, red lines, privacy settings and Hermes' own `config.yaml`
  (including which hooks run). Isolation protects what is **yours** (secrets,
  browser, Console key), not hpk's policies from a determined AI; making
  those files yours with read-only access for the AI is a follow-up.
- **Do not run `hermes` yourself while isolated.** It would load plugins,
  hooks and settings the AI can change, as you. Use
  `hpk isolate run -- hermes …`. The same goes for a Hermes gateway service
  installed to run as you: run it through hpk or as `hpk-ai`.
- **Hermes updates** need write access to its program folders, which the AI
  does not have: update with `hpk setup hermes`, or run teardown, update, setup.
- **`.env` files hpk writes into a project** are in a shared folder, so the AI
  can read them; hpk's hooks still refuse naming them. Keep secrets the AI must
  never see in the vault and use them in the browser by name.
- **Not tested end to end yet**: creating `hpk-ai` and starting Hermes as it
  needs an administrator on a real machine; `hpk isolate check` is the test to
  run after setup. Docker mode and the macOS/Linux manual steps are not tested.
