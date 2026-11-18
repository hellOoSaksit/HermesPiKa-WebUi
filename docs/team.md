# Team mode

By default hpk is **solo**: `hpk serve` listens on this machine only, with no
sign-in, and the person running it is the owner. Team mode lets several
people share one Console, each with a role. It is written from scratch and
uses no code from any Hermes web UI.

## Start it

```bash
hpk team init --owner olivia@example.com --name Olivia
```

This writes `<hermes home>/hpk/team.yaml` and prints Olivia's **personal
token** once. hpk keeps only its SHA-256, so a lost token cannot be shown
again. Issue a new one instead (`hpk team token <id>`).

Add people from the Console (Settings → Team, owners only) or the CLI:

```bash
hpk team add max@example.com --role member --project sample-app
hpk team list
hpk team role max@example.com maintainer
hpk team remove max@example.com
hpk team off      # back to solo; hpk team on to return
```

`--project` limits a person to those projects. They cannot see the others at
all.

## Roles

| Role | May |
|---|---|
| owner | everything: privacy, settings, budgets and money caps, notices, jobs, adding projects, promotions, people |
| maintainer | answer red-line approvals, change modes and red lines, approve core wiki pages (decisions/, architecture/, people/) |
| member | start and drive chats, claim work on the board, approve general wiki drafts (lessons/, versions/, glossary/) |
| viewer | read only |

A chat belongs to whoever started it. Others see it on the board; to let them
read it, its owner shares it (chat menu, **Share with…**). Owners of the Console
can read every chat. Every change is written to the project's `audit.log` with
the person's id.

## Money caps per person

Besides a project's caps (docs/budgets.md), an owner can cap what each
person's chats may cost, across projects:

```bash
hpk team budget max@example.com --daily 5 --monthly 60
```

or **Caps** on the Team page, which also shows what each person spent today and
this month. At a cap, that person's chats get no more tools and they cannot
start or continue a chat until an owner raises it or the day or month ends.
Scheduled jobs belong to the project, not a person, so only project caps apply
to them.

## Personal shared wiki

A page of a shared project can go to the team's shared wiki or to your own
(`hpk-shared/users/<you>/`): **Share to shared wiki** on the page. hpk makes a
redacted draft. An owner approves drafts for the team wiki; you approve your
own. Private and isolated projects cannot share pages at all.

## Signing in

**Tokens** (default). The Console asks for the token once and keeps a cookie
for 12 hours (`HttpOnly`, `SameSite=Strict`, `Secure` over TLS). Scripts can
send `Authorization: Bearer <token>`. Restarting `hpk serve` signs everyone
out.

**A reverse proxy that already signs people in** (oauth2-proxy, Authelia, …):

```bash
hpk team init --owner olivia@example.com --auth header --header X-Forwarded-Email --proxy 127.0.0.1
```

hpk reads the person's id from that header only on connections from the
listed proxy addresses. The id must be a member in `team.yaml`. List the
proxy's own address, not a range: every machine in a range can send the header
and claim to be anyone (`hpk team init` warns). For Docker, see docs/docker.md.

## Serving other machines

hpk listens beyond loopback only in team mode, and only with TLS:

```bash
hpk serve --addr 0.0.0.0:7878 --tls-cert server.crt --tls-key server.key
```

Behind a proxy that adds TLS, run hpk on loopback and let the proxy forward to
it. If the proxy must reach hpk over the network without TLS, say so with
`--insecure-http`. Changes still need the `X-HPK` header and a same-origin
request, so another site cannot act for a signed-in person.

## Join from your own hpk

The team server can run on its own (Docker, docs/docker.md) with nobody
opening its web page. Each person keeps hpk on their own machine and joins the
server from there; one person can join from several machines.

**Owner** — add the person, then make a one-time invite code (15 minutes):

```bash
hpk team add max@example.com --role member
hpk team join-code max@example.com      # prints e.g. K7QPX-M2RTD
```

or **Invite code** next to the person on the Team page. An owner who has
already joined can also do this from their own hpk: Settings > Team server.

**The person** — in their own hpk: Settings > **Team server**, enter the
server's address (`https://…`), the code and a name for this device, then
**Join**. hpk exchanges the code for a **device token**:

- The code works once and only until it expires; wrong codes are slowed down
  and an address that sends 10 wrong codes in 10 minutes is blocked for a while.
- The server keeps only the token's SHA-256 (`<hermes home>/hpk/devices.json`, 0600).
- The person's hpk keeps the token in their vault as an hpk-only secret: it is
  not in the vault list, the AI cannot name, read or fill it, and the Console
  never sees it. `<hermes home>/hpk/teamserver.json` says only where and who.
- The Console on the person's machine reaches the server through their hpk,
  and only for a fixed list of things (people, board, approvals, wiki pages,
  devices, invite codes). The person's role on the server still decides what
  they may do.

Device tokens work with either sign-in mode. In header mode, the proxy must pass
the `Authorization` header through to hpk for `/api/` requests.

Once joined, the page shows the connection, the team's people, open claims and
waiting approvals, and **My devices**. Built-in browser, vault and local MCP
servers stay on the person's machine; a team server refuses them and its
Console hides them.

### Devices

Every joined machine is listed with when it was last seen and from which
address:

- the person: Settings > Team server > **My devices** (revoke any of their own);
- owners: Team page > **Joined devices** (revoke anyone's), or
  `hpk team devices` and `hpk team revoke-device dev_…`.

A revoked device stops working at once. Removing a person from the team stops
all their devices. **Leave this server** on a machine revokes that device and
deletes its token (the vault PIN is asked for when one is set).

### TLS and an address allowlist

Joining needs HTTPS: the server refuses `/api/join` over plain HTTP from the
network, and a person's hpk accepts only `https://` addresses (plain HTTP only to
this same machine, for testing). Certificates are checked as usual and
redirects are not followed, so the token cannot be sent elsewhere.

To let only some networks reach the server at all:

```bash
hpk team allow-ip 203.0.113.0/24 198.51.100.7   # IPs or CIDR ranges
hpk team allow-ip                               # show it
hpk team allow-ip --clear
```

This sets `allow_ips` in `team.yaml`. Every request from another address gets 403,
including sign-in and joining. Loopback is always allowed. Behind a proxy
listed in `trusted_proxies`, the address is the last one the proxy added to
`X-Forwarded-For`.

### Security rules (2026-11-18 review)

- **Behind a TLS proxy, list it in `trusted_proxies`.** `X-Forwarded-Proto: https` is believed
  only from those addresses; without it the sign-in cookie is not marked Secure and `/api/join`
  refuses plain HTTP from the network.
- **Sign-ins end when a token changes.** Rotating a member's token, or removing them (also with
  `hpk team remove`), ends their Console sign-ins at once and drops their devices and pending
  invite codes. Re-adding the same id brings none of them back. Open live streams re-check who they
  are for every 25 seconds and close when that person can no longer sign in.
- **Chats stay in their project and with their owner.** A chat id is reachable only through its own
  project, and only by its owner, the people it is shared with, or a Console owner. Red-line
  approvals are answered only through their own project.
- **A person's own hpk never makes credentials for the team.** Invite codes, adding or removing
  people, roles, revoking someone else's device and approvals are done in the team server's own
  Console; the local hpk (which any program on that machine can call, the AI included) only reads
  and manages the person's own devices. The device token is kept in the vault together with the
  server address it was issued by and is sent nowhere else, whatever `teamserver.json` says.
- **Without team mode the Console answers only to `localhost`, `127.0.0.1` or `[::1]`**, so a
  web page that points its own domain at 127.0.0.1 (DNS rebinding) is refused.

## Tested

On Linux (Alpine, in Docker), with this setup:

- `hpk serve --tls-cert --tls-key` on `0.0.0.0`
- tokens
- a reverse proxy container in header mode

Results:

- Requests without sign-in get 401.
- A viewer trying to change something gets 403.
- A request from another site is refused.
- The cookie is `HttpOnly; Secure; SameSite=Strict`.
- Plain HTTP on the TLS port is refused.
- A forged identity header sent directly to hpk (not through the proxy) gets 401.
- Solo mode refuses to listen on the network, and team mode refuses to without TLS.

## Not built yet

- Sending a joined machine's work to the server (claims, reports, wiki drafts)
  and linking local projects to team projects: docs/plans/connected-mode.md.

- OIDC and passkeys built into hpk. Use header mode behind a proxy that does
  OIDC.
