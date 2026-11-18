---
name: hpk-wiki
description: Read and grow this project's wiki through hpk. Use it before answering questions about why the project is the way it is, and after finishing work that taught something worth keeping (a decision, a lesson, how parts connect, what a version changed). Never use it for rules or current status, which live in the repository.
---

# Project wiki (hpk)

The wiki is this project's **accumulated knowledge**. It lives at `$WIKI_PATH`. All commands below cost 0 tokens: they are file and git work done by `hpk`.

## Before you answer or plan

1. Read `$WIKI_PATH/index.md` (one line per page).
2. If the index does not answer it, search: `hpk wiki search <words> --json`.
3. Open only the pages you need. Cite them (path) when you rely on them.

If the wiki and the repository disagree, the repository wins. If the person in the chat says otherwise, they win.

## When you learned something worth keeping

Write a **draft**. You never write pages directly and you never approve your own drafts.

```sh
hpk wiki draft --target lessons/<short-name>.md \
  --title "<page title>" \
  --summary "<one line: what this adds>" \
  --source "<file>:<line>" --source "<commit or session id>" \
  --confidence medium --author "<your session id>" <<'EOF'
<page body in Markdown, under ~1,500 tokens>
EOF
```

- **Where:** `lessons/` (problems and fixes), `decisions/` (choice + reasons + options not taken), `architecture/` (parts and connections), `versions/` (what a version or lane changed), `glossary/` (terms, customers), `people/` (who looks after what).
- **Sources are required.** Every claim needs a file:line, a commit or a session id. hpk refuses a draft without one.
- **No rules.** If what you found is a rule ("always…", "never…"), propose it for the repository instead.
- **No secrets.** hpk refuses drafts that look like keys, passwords or tokens.
- **Editing an existing page:** use its path as `--target`; write the whole new body.
- Keep it short. Split anything longer than about 1,500 tokens.

Then tell the person, in one line, which draft number is waiting (e.g. "Draft 4 → lessons/csv-encoding.md is waiting for your approval").

## Raw material and the log

- Hand untouched source material (session summaries, extracts) to hpk, which stores it under `raw/`. It needs no draft: `hpk wiki raw sessions/<date>-<topic>.md <<'RAW' … RAW`. Do not write into the wiki folder yourself. It sits inside the Hermes home, which Hermes guards.
- Record notable events with `hpk wiki log "<one line>"`. The log is append-only.
- In a long session, save a short summary before the context fills up: decisions made, what was tried and failed, open questions, each with its source. Use `hpk wiki raw sessions/<date>-<topic>.md`. When Hermes compacts the context, the details it drops are gone, and hpk only learns that it happened.

## What you must not do

- Do not run `hpk wiki approve` or `hpk wiki discard`. A person decides (Auto mode may approve lessons, versions and glossary only, through hpk, never through you).
- Do not edit `SCHEMA.md`, `index.md` or `log.md` by hand.
- Do not copy wiki content to other projects or to the shared wiki; promotion needs a person (red line).
