# Built-in browser and vault

hpk can give the agent a browser on **your own machine**, and keep the secrets it needs
(passwords, logins, tokens, `.env` values) so that the AI uses them **without ever seeing them**.
Both are off on a team server: the server runs no programs and keeps no one's passwords.

## What you see

- **Browser panel**: the globe button in the top bar docks a live view on the right or along
  the top of any page. It opens by itself when the AI starts browsing (Settings on the Browser
  page). You see the address, what the AI is doing now, a ripple where it clicks, and every step.
- **Take over**: one click hands the browser to you; click, type, scroll and paste go to the
  page. While you drive, the AI's browser tools answer "the person is using the browser". Hand it
  back when you are done.
- **Work > Browser**: the same view full size, plus **Recordings**: every browsing session with a
  screenshot after each step. Step through them, pin one to keep it past 30 days, or delete it.
  Recordings are private to you and never go into the wiki.
- **Settings > Vault**: your secrets. A value is shown as dots; the AI sees only
  `{{secret:name}}` and the sites it may be used on.

## How the AI uses a secret

1. It calls `secrets` and gets names only, e.g. `github-login (login) sites: github.com`.
2. It calls `fill_secret` with `{{secret:github-login.user}}` / `{{secret:github-login}}` and the
   ref of the field.
3. hpk checks the page against the secret's sites: `example.com` is that host only, `*.example.com`
   also covers its subdomains (opt-in, so allowing a big site does not open every page under it);
   the page must be https on the normal port (plain http only for localhost).
4. In one step inside the page, hpk re-checks the site, checks the field (a password goes only
   into a password field; a user name only into a text/e-mail field of a sign-in form), masks it,
   then sets the value. It never types into "whatever has focus".
5. Anything the AI reads back (page text, URLs, titles, errors, the live state) is scrubbed so the
   value appears as `{{secret:name}}` — also its JSON-escaped, URL-encoded, re-cased and cut-short
   forms. On a page holding a filled field, snapshots show no field values, and `wait`/click by
   text are refused (they could test the value one guess at a time).
6. The person cannot fill secrets through the Console API; only the AI asks, by name.

## The vault

- Stored in `<hermes home>/hpk/vault/vault.bin`, AES-256-GCM. The key is protected for your OS
  account: DPAPI on Windows, the Keychain on macOS, the Secret Service on Linux; a 0600 key file
  where none of these exist (the Console says so).
- **PIN**: set before the first secret (8+ characters, 10+ if digits only). Changing, deleting or
  showing a secret, and writing `.env` files, need the vault unlocked with it (10 minutes). The
  unlock is a token the Console keeps in memory and sends as a header — not a cookie, which a
  browser would also send to other localhost ports. The PIN check and the wrong-try counter are
  sealed inside `vault.bin` with the secrets; tries are counted before the slow hash, 5 wrong tries
  lock it for 5 minutes (longer each time), and a restart does not reset the lock. Adding a new
  secret needs no unlock (it cannot leak an old one).
- **.env values**: an env secret belongs to a project. *Write .env* puts the values into the
  project's `.env` (other lines are kept); it refuses to write through a link. hpk then refuses
  agent tool calls that name that file.

## Connecting the AI

Settings > Vault > *Connect to Hermes* adds the `hpk-browser` MCP (`hpk mcp-browser`, stdio).
It holds no browser and no secrets: it forwards each tool call to the running `hpk serve`, using
the address and key in `<hermes home>/hpk/browser/agent.json`. Tools: `open`, `snapshot`, `click`,
`type`, `fill_secret`, `secrets`, `press`, `scroll`, `back`, `forward`, `reload`, `wait`,
`screenshot`.

The browser is Chrome, Edge or Chromium (`HPK_BROWSER` overrides), headless, with its own profile
under `hpk/browser/profile`, so its cookies and logins stay apart from your own browser. Chrome's own
password saving is off in that profile. hpk only talks to a browser it started itself (a leftover
one on the profile is closed first); new tabs are closed and a page's popup is opened in the one tab
and recorded as the page's doing.

## Limits (read this)

**DevTools is a pipe.** hpk talks to the browser over `--remote-debugging-pipe` (fd 3/4 on
Linux and macOS, inherited handle numbers on Windows): no port, so no other program or OS account
can connect. If a browser does not answer on the pipe, hpk falls back to a port on 127.0.0.1 only
while the AI runs in your own account; with the AI isolated it refuses.

**By default the AI runs programs as your OS account.** hpk keeps values out of everything it
shows the AI, blocks agent tool calls that name the vault, the browser profile, recordings,
`agent.json`, the vault API or a managed `.env`, and requires the PIN for anything that could
leak or redirect a secret. That stops accidents and casual attempts. It is **not a sandbox**: the
OS protects the vault key per account (DPAPI, Keychain, Secret Service), so a program written to
dig can still decrypt the vault or read what your account can read. `hpk doctor` warns about this.

**The fix is `hpk isolate setup`** ([isolation.md](isolation.md)): Hermes then runs as a separate
local account (Windows) or in a container, and your vault, its key, the browser profile and the
Console key move to a folder only your account can open. The AI shares only the Hermes home and
your project folders. Known limits there: the AI can still change hpk's policy files in the shared
home, and `.env` files hpk writes into a project are readable by it.
