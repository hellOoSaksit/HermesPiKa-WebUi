# Roadmap

hpk v1 is **solo-first**: one person, Hermes Agent on their own machine. The
team pages in the Console are greyed out ("Comes in a later version") and the
APIs that start new team joins answer 404 in solo mode. A server started with
`hpk serve --team` is not affected, and a machine that already joined a team
server keeps its Team server page. The plans below are written (in Thai) but
not shipped.

## Connected mode

A person's own hpk talks to a team server. Plan:
[docs/plans/connected-mode.md](plans/connected-mode.md), section 10, steps 1 to 6.

1. Server login and status, `servers.json`, the server's `/api/remote/hello`.
2. Project link and unlink, an outbox and `hpk sync`, claims and heartbeats
   sent to the team, a clash check against the team before taking a task.
3. Reports and wiki drafts go up to the team; team wiki, red lines and mode
   come down (the stricter one wins).
4. Team approval (`team_approval`) and one-time permits.
5. Team AI account relay with budgets and rates, `use-team-models`, and the
   team/personal route.
6. Console for both sides, and `docs/connected.md`.

## Git sync

Plan: [docs/plans/git-sync.md](plans/git-sync.md), section 11.

- G0: new branch names, `git.base`, `git.allowed_remotes`; push to base and
  force-push are refused.
- G1: `hpk sync` (fetch), notice when the base moves or files clash, an
  update-branch button.
- G2: push and a draft PR/MR from a sub-task report through `gh`/`glab`,
  after approval.
- G3: webhook and PR index on the server; CI results become evidence; a merge
  closes the claim and drafts a wiki note.
- G4: warn about clashes with other people's PRs before claiming or pushing.
- G5: the whole Console side and `docs/git.md`.

## Sign-in

OIDC and passkeys for team mode, next to personal tokens and the reverse-proxy
header.

## Isolation

`hpk isolate setup` on macOS and Linux (v1 ships Windows; see
[isolation.md](isolation.md)).

## Updates from inside hpk

Plan: [docs/plans/auto-update.md](plans/auto-update.md). Today a person
updates by installing again. Planned: a daily check, a banner with release
notes, an **Update** button and `hpk update` that verify the checksum and
reuse self-install, a refresh of the hpk copy that Hermes' hooks run, and a
one-step rollback. Never silent.

## Follow-ups found in review (0.1.0)

Not blocking 0.1.0, worth doing next:

- **Upgrades:** the copy of hpk in `<hermes home>/hpk/bin` (used by the hooks
  and the isolated AI) is not refreshed when hpk is updated (part of the
  update plan).
- **Uninstall:** `hpk self-uninstall` reminds you to run `hpk uninstall --yes`
  but does not run it.
- **Install scripts:** `HPK_NO_START` means "set" in install.ps1 and "=1" in
  install.sh. There is no success-path test for install.ps1. The Start-menu
  shortcut quoting does not double "smart" quotes (pass the values through
  environment variables instead). A failure to edit PATH or the profile
  should be a warning, not an abort. Leftover `hpk.old.exe`/`.gone` files
  should be cleaned on the next install.
- **Release:** sign the binaries. Pin the compose image tag per release (see
  docs/release.md).
- **Files:**
  - The first render of the Files view waits for the full tidy walk.
  - An apply that hits a 409 shows a bare "Conflict".
  - Card counts include direct files only.
  - Place names in the folder picker are English only.
  - Tidy has no per-project lock.
  - The undo record is written once, at the end of a round.
  - Path checks are path-based, not handle-based: tidy verifies after
    each move and puts the file back, but the window still exists.
- **Hooks:** session `.folders` files are never cleaned up. The folder-rule
  cache filename can exceed 255 bytes for very long project names.
- **macOS/Linux:** run the self-install profile tests on a real machine, and
  try the built-in browser and the Keychain or Secret Service there.

Turn `features.team` back on (`HPK_FEATURES=team`) once the team work above is done.
