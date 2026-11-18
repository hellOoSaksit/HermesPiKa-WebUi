# Switching models in a chat

A Console chat can move to another model without losing its memory, or ask
another model one question and keep its own. Code: `internal/modelswitch`
(the plan), `internal/serve/modelapi.go` (the API), `assets/console/js/views/chat.js`
(the picker).

## How Hermes switches

hpk drives `hermes-acp`. `session/new` returns the models Hermes offers the
session (`provider:model` ids) and the current one. `session/set_model` rebuilds
the session's agent on the new model and keeps the session's history, the same
path as `/model` in a Hermes chat. It works only while the session is idle and
never writes Hermes' config. Hermes' message converters drop thinking blocks the
new model cannot replay (signed thinking from another model); the answers stay.

`/compress` in the session is Hermes' own compaction: a summary of older turns
plus the most recent messages (`protect_last_n`, 20 by default). Hermes keeps
the full transcript in its session store, so it stays searchable.

## What hpk decides

The picker (composer chip → *Switch model or ask once…*, or `/model`) asks
`GET /api/sessions/{id}/models`. For every model Hermes offers it shows:

- **a ring**: how full this chat's history would be in that model's window.
  When the switch compacts, the full history shows faint and what is actually
  sent shows solid.
- **the first-turn cost**: the tokens the first message sends, at the model's
  input (or cache-write) price from the models.dev cache, because nothing is
  cached on a new model yet.
- **a red-line badge** for a model from another provider, and a lock on a
  `model_policy: local_only` project for anything not on this machine.

History size is Hermes' own estimate (`usage_update` over ACP) when it has sent
one, else hpk counts the chat log. Windows come from `limit.context` in the
models.dev cache; an unknown model is assumed to have 128k.

Auto picks how the history travels:

| Full history (with brief and recall) | What happens |
|---|---|
| ≤ 60% of the new window | **Whole history**: `session/set_model` only |
| > 60% | **Compact**: `/compress` on the old model (its cache is warm), then `session/set_model` |

Either way the session brief (`briefs/<id>.md`) and the wiki pages recalled
into the chat (up to 4,000 tokens; pages over the budget are named, not sent)
go in front of the next message as one `[hpk] This chat moved from … to …` block,
once.

## Rules

- Same provider: no approval.
- Another provider: the `model` red line. The person confirms in the Console;
  the server refuses an unconfirmed request (409) and audits the approval.
- `local_only` projects: only local models (loopback or `privacy.local_hosts`).
  The server refuses others even when confirmed (403).
- The chat must be idle. While the switch runs, the chat shows as busy and new
  messages wait in Hermes' queue.

Every switch is recorded on the session (`switches` in board.db) and in the
project's audit log. The chat shows a divider, *Switched to opus here*, and the
latest one has a one-click *Back to sonnet*.

## Ask once

*Ask once* (or `/ask opus <question>`) starts a sub-task of the chat on the
other model, with no worktree. Its brief carries the question; it answers, sends
the usual report, and the report lands in the main chat as a summary. The main
chat keeps its model and its prompt cache. The same provider and local-only rules
apply.

## API

| Call | Body | Answer |
|---|---|---|
| `GET /api/sessions/{id}/models?recall=a.md,b.md` | | `live`, `busy`, `current`, `policy`, `state`, `options[]`, `switches[]` |
| `POST /api/sessions/{id}/model` | `model`, `confirm`, `recall[]` | `way`, `carry`, `firstCost` |
| `POST /api/sessions/{id}/ask` | `model`, `text`, `confirm` | the new sub-task |
