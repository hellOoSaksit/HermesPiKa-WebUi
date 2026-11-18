# Budgets, costs and notices

hpk never calls a model itself. It counts what Hermes reports and stops work at
the limits a project sets. All the numbers below come from hooks hpk installs
in Hermes (see install.md).

## What is counted

After every request, main or auxiliary (titles, compression), Hermes reports
usage to the `model_usage` hook. hpk records it per Hermes session in
`<hermes home>/hpk/routes/`:

- input, output, cache-read and cache-write tokens
- the cost in USD, at the prices in the models.dev cache Hermes keeps
  (`models_dev_cache.json`; hpk extracts them to `hpk/prices.json`)

The Console shows each chat's tokens and cost, the project's spend today, and
its cost over 30 days. A model missing from the price cache costs $0 there.
Its tokens still count.

## Token budgets

| `project.yaml` | What happens |
|---|---|
| `budget.ingest_max_tokens` | reading documents into the wiki stops before the source that would pass it |
| `budget.nightly_max_tokens` (also weekly, digest) | a run that passes it is stopped at its next tool call, then the job pauses until a person allows it (`hpk cron allow`) |
| `budget.cron_daily_max_tokens` | every job of the project pauses when its jobs together pass it in 24 hours |

Budgets count **weighted tokens**: input + cache writes + output + one tenth of
cache reads. Cache reads cost about a tenth of input on the providers Hermes
supports, and an agent re-reads its cached context on every turn. In the first
real run, a nightly job used 346,395 tokens by Hermes' raw count. 302,515 of
those were cache reads. Weighted, it was about 74,000, which tracks the real
cost of about $0.22 at list price. When hpk has no hook record for a run, it falls
back to Hermes' raw total.

Hermes has no per-job limit, so hpk stops a run by refusing its tools: the
guard tells the agent the run is over budget and asks it to report and stop.
Its last answer still costs a little.

## Money caps

`budget.money_daily_usd` and `budget.money_monthly_usd` (0 = no cap). Set them
in the Console under Settings. When a project's spend reaches a cap:

- agents get no more tools on that project
- the Console does not start new sessions
- scheduled jobs pause

This lasts until a person raises the cap or the day or month ends (local time).
A session counts whole on the day of its last request.

In team mode an owner can also cap each **person** (docs/team.md): what the
chats they start cost, across projects. Both caps apply; the first one reached
stops the work.

## Notices

The Console always shows every notice. A **shared** project can also send
them to a messaging platform through Hermes:

```yaml
notify:
  to: telegram          # or slack:#builds, discord:#ops … (a `hermes send --to` target)
```

hpk runs `hermes send`, so the platform's token stays in Hermes. The Console's
Settings lists the targets Hermes knows (`hermes send --list`) and has a
**Send a test** button. hpk sends a notice when:

- the agent waits for a person (a red line or another approval)
- a chat finishes a turn

Each kind goes out at most once every 5 minutes. Duplicate-work alerts are
never held back.

Private and isolated projects never send notices out: their content stays on
this machine, and the Console refuses a target for them.

## Limits

- Cost uses list prices. Discounts, batch pricing and provider-side caching
  rules can differ.
- Usage hpk did not see (sessions run before the hooks were installed, or in
  another Hermes home) is not counted.
