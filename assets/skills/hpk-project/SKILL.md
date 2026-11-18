---
name: hpk-project
description: Add an existing project to hpk and fill its wiki from the project's own documents, one source at a time under a token budget. Use when the person asks to add, analyse or onboard a project folder, or to continue an add that stopped. Always ask before reading anything.
---

# Add a project (hpk)

hpk does the free work (scan, estimate, queue, health checks). You do the reading and writing that needs a model, one queued file at a time. Stop whenever hpk says to stop.

## 1. Ask first. Never skip this.

```sh
hpk project add <folder>
```

Without `--yes` this only lists file names and sizes and prints the choices and the estimated cost. Exit code 3 means "needs consent". Show the person:

- **depth:** full / light (README + docs) / skip
- **privacy:** shared / private / isolated
- the estimated tokens and cost
- that secret files are never opened

Wait for their answer. If they want to leave something out, add `--exclude <glob>` (for example `--exclude "docs/old/**"`). Then run what they chose:

```sh
hpk project add <folder> --depth light --privacy private --yes
```

If hpk says the plan is over budget, tell the person the numbers. Let them cut sources or raise `--budget`. Do not decide this yourself.

## 2. Read the queue (step "ingest")

Repeat until the queue is empty:

```sh
hpk project ingest <name> --next --json
```

1. Read the file named in `file`. Read only that file. Never open `.env`, keys or credential files, even if a document points to them.
2. Hand the facts you found to hpk and mark the file done, in one command. hpk writes them to the wiki page named in `raw`. **Do not write into the wiki folder yourself:** it sits inside the Hermes home, which Hermes guards. Use this format:
   - front-matter with `title`, `type: raw`, `sources: [<source path>]`, `updated`, `confidence`
   - short bullet facts, each ending with its source as `(path:line)`
   - no opinions and no rules

   ```sh
   hpk project ingest <name> --done <path> --tokens <tokens you used> --notes - <<'NOTES'
   ---
   title: <source path>
   type: raw
   sources: [<source path>]
   updated: <today>
   confidence: high
   ---
   - <fact> (<source path>:<line>)
   NOTES
   ```

   hpk refuses notes that look like they contain a secret, and writes nothing.

   If a file is noise (generated, a duplicate, empty), mark it `--skip <path>` instead.

**Exit code 4 means the budget is used up.** Stop. Tell the person how much was used and what is left, and ask whether to raise the budget or skip the rest.

## 3. Organise (step "organise")

From `raw/ingest/` and `raw/git-history.md`, draft a small number of pages through the wiki:

- `architecture/overview.md` covers the parts of the system and how they connect.
- `glossary/<term>.md` covers terms, customers and organisations.
- `decisions/<topic>.md` covers decisions the documents explain: what was chosen, why, and what was not chosen.
- `lessons/<topic>.md` covers problems the history shows were solved.

Use `hpk wiki draft …` with `--source` pointing at the original files and lines (see the hpk-wiki skill). Keep each page short.

Then move on:

```sh
hpk project step <name> health
```

## 4. Health (step "health")

```sh
hpk project health <name> --json
```

This lists broken links, files the docs mention but that do not exist, rules repeated across agent instruction files, and folders no document mentions. Add anything you noticed while reading, such as a document that contradicts the code, citing both places.

Show the list to the person as numbered items and ask which ones to fix. **Changing files in their repository is a separate request.** Never edit AGENTS.md, CLAUDE.md or similar files yourself (red line).

Then run:

```sh
hpk project step <name> done
```

## Resume

If anything was interrupted, `hpk project resume <name>` says which step it stopped at and what to do next.
