# project.yaml

`hpk project add <folder>` writes a project's settings to
`<hermes home>/hpk/projects/<name>/project.yaml`, and you can edit the file
freely afterwards. hpk keeps any keys it does not know, and it only rewrites
the file when you change privacy through `hpk project privacy`. That command
edits just that one key and keeps your comments.

```yaml
name: shop-api                # lowercase letters, digits, . _ -
root: ~/code/shop-api         # the project's folder; chats whose cwd is inside it belong here
language: en

privacy:
  mode: shared                # shared | private | isolated (see privacy.md)
  model_policy: any           # any | local_only (privacy.md, Local models only)
  local_hosts: []             # hosts besides loopback that count as this machine for local_only

board:
  kind: hpk                   # hpk = board.db; markdown = also keep an existing board file
  file: ""                    # path of that file when kind is markdown
  stale_after_minutes: 30     # a claim without a heartbeat for this long counts as stale

worktrees:
  dir: worktrees/             # where sub-tasks get their own git worktree

gates:
  default_mode: ask           # manual | ask | auto
  red_lines_extra:            # more things that always need a person
    - "re:^terraform apply"   # "re:" + a regular expression on the command

checks:
  test: go test ./...         # what "verified" means before a sub-task can merge

ingest:
  depth: light                # skip | light (README and docs) | full
  include: []
  exclude: []                 # .env and secret files are skipped whatever you put here

budget:
  ingest_max_tokens: 40000    # the first analysis stops here
  nightly_max_tokens: 60000   # a nightly run is stopped mid-way past this (docs/budgets.md)
  cron_daily_max_tokens: 150000  # every job pauses when the project's jobs together pass this in 24 h
  money_daily_usd: 0          # 0 = no cap; at a cap agents get no tools and jobs pause
  money_monthly_usd: 0

notify:
  to: ""                      # also send notices with `hermes send --to <this>`; shared projects only

delivery: console             # where reports go; private and isolated projects always stay local
```

## Modes

| Mode | What runs without asking |
|---|---|
| `manual` | reads only; every write and every command waits for a person |
| `ask` | reads, commands, and edits inside the project; a write outside the project asks |
| `auto` | everything except red lines; falls back to `ask` when `--for` runs out |

The built-in red lines apply in every mode, and `red_lines_extra` cannot remove
them:

- push
- delete
- secrets
- control files, including Hermes' config
- model change
- the wiki core
- promote or privacy change
- egress

You can change the mode for a while with `hpk gates mode <mode> --for 2h`.
Every decision is written to `audit.log`.
