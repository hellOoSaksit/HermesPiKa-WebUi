# Running HPK Console in Docker

The image holds hpk only: the Console, its API and the team sign-in. It runs
as a non-root user, works with a read-only root filesystem and no Linux
capabilities, and keeps everything in the `/data` volume (the Hermes home hpk
uses).

## Quick start (one person, one computer)

Use the released image with `compose.release.yaml`, which is attached to every
release (`ghcr.io/helloosaksit/hpk-hermes:0.1.0`, Hermes Agent and hpk
together, the Console on http://127.0.0.1:7878). In an empty folder, download
it (macOS/Linux):

```bash
curl -fsSLO https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/compose.release.yaml
```

On Windows (PowerShell):

```powershell
irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/compose.release.yaml -OutFile compose.release.yaml
```

Change `HPK_OWNER` in the file to your email, then run these in that folder:

```bash
docker compose -f compose.release.yaml up -d
```

Wait a few seconds for it to start. If the token file is missing, wait a
little and run it again:

```bash
docker compose -f compose.release.yaml exec hermes cat /opt/data/hpk/owner-token
```

Open http://127.0.0.1:7878 and paste the token, then delete the token file:

```bash
docker compose -f compose.release.yaml exec hermes rm /opt/data/hpk/owner-token
```

Hermes needs a model account:

```bash
docker compose -f compose.release.yaml exec -u hermes hermes hermes auth add <provider>
```

In Git Bash on Windows, put `MSYS_NO_PATHCONV=1` before these commands. Data
lives in the `hermes-data` volume. For a team server with TLS, read on. Beginner
guide: [install.md](install.md) (Thai: [install.th.md](install.th.md)).
Publishing a release (maintainers): [release.md](release.md).

## Build

The Dockerfile packages a Linux binary built outside the image, so the image
needs no Go toolchain:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/docker/hpk-linux-amd64 ./cmd/hpk
docker build -t hpk:local .
```

For arm64, build `hpk-linux-arm64` and pass `--build-arg TARGETARCH=arm64`.

## Run with TLS

```bash
docker run -d --name hpk -p 7878:7878 -v hpk-data:/data -v /path/to/certs:/certs:ro \
  -e HPK_OWNER=you@example.com -e HPK_TLS_CERT=/certs/tls.crt -e HPK_TLS_KEY=/certs/tls.key \
  --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges:true hpk:local
docker exec hpk cat /data/hpk/owner-token
```

On first start, `HPK_OWNER` turns team mode on (docs/team.md). The owner's
token goes to `/data/hpk/owner-token` (mode 600), never to the log. Read it
once, then delete it. Later starts ignore `HPK_OWNER`.

Manage people with hpk inside the container:

```bash
docker exec hpk hpk-entrypoint team add max@example.com --role member
docker exec hpk hpk-entrypoint team join-code max@example.com   # one-time code for their own hpk
```

People do not need to open this server's web page: each joins from their own
hpk (Settings > Team server) with the server's address and an invite code. See
"Join from your own hpk" in docs/team.md.

The image's health check calls `/api/health`.

## Behind a reverse proxy

`docker/compose.yaml` runs hpk on an internal network with
`HPK_BEHIND_PROXY=1` (plain HTTP to the proxy). Publish only your proxy, which
must:

- terminate TLS and send `X-Forwarded-Proto: https`, so the sign-in cookie is
  marked `Secure`
- keep the browser's host in `Host` or `X-Forwarded-Host`, so hpk can tell
  same-site requests from cross-site ones

With **header sign-in** (`HPK_AUTH=header`), set `HPK_PROXY` to the proxy's own
address and give the proxy a fixed address. If you trust a range, every
container or machine in it can send the header and claim to be anyone. The
Docker test showed this, and `hpk team init` warns about it.

## Tested

On Docker Desktop (Linux containers), with this setup:

- the image above, with TLS, and through compose behind a TLS proxy
- tokens and header sign-in
- a read-only root filesystem, no capabilities, running as `hpk` (uid 100)
- a project with its wiki (git) created inside the container

Results:

- The health check reports healthy.
- No sign-in gets 401, and a viewer trying to change something gets 403.
- Cross-site requests are refused.
- The cookie is `HttpOnly; Secure; SameSite=Strict`, through the proxy too.
- Data survives a restart and a new container on the same volume.
- Without team mode, the container refuses to listen on the network.

## Hermes and hpk in one container

The image above runs the Console, team sign-in, the board, the wiki and the
budgets. Chats and scheduled jobs also need Hermes Agent. `docker/hermes/`
builds on the official Hermes image (`nousresearch/hermes-agent`) and adds
hpk to it:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/docker/hpk-linux-amd64 ./cmd/hpk
docker build -f docker/hermes/Dockerfile -t hpk-hermes:local .
```

On every start, before Hermes' services:

1. `hpk install --yes` adds hpk's hooks and skills to `/opt/data/config.yaml`.
   Running it again changes nothing; a backup is kept the first time.
2. The hooks are allowed in Hermes (you chose this image, so its hooks are
   expected). `tools/hermes-consent.py` does it with Hermes' own functions.
3. On the first start, `HPK_OWNER` turns team mode on; the token goes to
   `/opt/data/hpk/owner-token`.

Then the Hermes gateway runs as the main process, and HPK Console runs beside
it as an s6 service, as the `hermes` user. Without team mode the Console does
not start, and says why in the log. `docker/hermes/compose.yaml` runs it behind
a proxy.

Hermes needs a model account before chats or jobs can run. An owner can add
one from the Console: **Settings → AI accounts → Add an API key**. The page
also reorders, resets and removes accounts. Accounts that sign in through a
browser (Claude, Codex, Nous) are added in a terminal:
`docker compose exec -u hermes hermes hermes auth add <provider>`.

How a key is handled:

- hpk runs `hermes auth add` and hands it the key on stdin. The key is never
  in a command line, a log or a file of hpk's; Hermes stores it.
- The Console accepts a key only over HTTPS (direct, or at a proxy that sends
  `X-Forwarded-Proto: https`) or from a browser on the same machine.
- Only owners manage accounts. The log line says what changed and who did
  it, never the key.
- On Windows, Hermes reads keys from the keyboard, so keys are added in a
  terminal there. Reorder, reset and remove still work from the Console.

### Joining from your own hpk

People join with an invite code from their own hpk (Settings > Team server).
The server takes a code only over HTTPS, or from its own loopback. So the
plain-HTTP try-out (`compose.local.yaml`, port 7880 on this computer) cannot be
joined from the hpk on that same computer: the request reaches the container
from Docker's network, not from loopback. Put the TLS proxy from
`compose.yaml` in front (a certificate your machine trusts), or, to try the
flow only, run a second hpk inside the container and join
`http://127.0.0.1:7878` from there.

### Tested

On Docker Desktop with `nousresearch/hermes-agent:v0.23.1` (4.56 GB; hpk adds
about 20 MB):

- setup ran, and `hermes hooks doctor` reported all hpk hooks healthy
- `hpk doctor` was all green (Hermes, `hermes-acp`, git, hooks, skills,
  consent)
- an ACP handshake with `hermes-acp` worked
- a `git push` sent through Hermes' hooks reached hpk's `red_line_guard`,
  which asked a person (0.2 s)
- the container was healthy, and requests without sign-in got 401
- after a restart, setup ran again without duplicating anything, and the owner
  token still worked

Account management was tested in the container:

- two obviously fake keys were added through the page
- one was moved up, the pool was reset, and both were removed

Afterwards no log, file in `/opt/data` or process command line contained the key.
Chats were not run: a fake key cannot call a model.
