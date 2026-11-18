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

Turn `features.team` back on (`HPK_FEATURES=team`) once the team work above is done.
