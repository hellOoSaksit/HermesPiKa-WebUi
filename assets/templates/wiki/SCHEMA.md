---
title: Wiki schema
type: schema
sources: [hpk template]
updated: {{DATE}}
confidence: high
---

# How this wiki works ({{PROJECT}})

This wiki holds **accumulated knowledge** about {{PROJECT}}: why things are the way they are, how the parts fit, what went wrong and how it was fixed. Rules and current status live in the project repository, not here.

## Folders

| Folder | Holds | Who writes |
|---|---|---|
| `raw/` | Untouched source material: session summaries, git logs, extracts | the agent, freely |
| `decisions/` | A decision, its reasons and the options not taken | draft → a person approves |
| `architecture/` | Parts of the system and how they connect | draft → a person approves |
| `versions/` | What each version or lane changed | draft → approve (Auto may approve) |
| `lessons/` | Problems met and how they were solved | draft → approve (Auto may approve) |
| `glossary/` | Terms, customers, organisations | draft → approve (Auto may approve) |
| `people/` | Who looks after what, how they like to work | draft → a person approves |
| `_drafts/` | Drafts waiting for approval (`stale/` after 14 days) | the agent |
| `SCHEMA.md` | This file | a person only |

## Page rules

1. Every page starts with front-matter: `title`, `type`, `sources`, `updated`, `confidence` (low | medium | high).
2. **Every claim has a source**: a file and line, a commit, or a session id. No source, no page.
3. Keep a page under about 1,500 tokens. Split longer pages.
4. No rules here. If you find a rule, propose it for the repository instead.
5. Never put secrets, keys, passwords or `.env` content in the wiki.

## Workflow

- Read `index.md` first, then search (`hpk wiki search`).
- New knowledge goes in as a draft (`hpk wiki draft`); a person approves it (`hpk wiki approve`).
- Every change is appended to `log.md` and committed to git.
