# Privacy

Each project has its own wiki. In addition there is one shared wiki
(`<hermes home>/hpk-shared/`) for lessons that are useful across projects. A
project's privacy mode decides whether it may take part in the shared wiki.

| | Reads shared wiki | Writes shared wiki | Content may leave this machine |
|---|---|---|---|
| `shared` (default) | yes | yes, by promoting a page after a person approves it | yes |
| `private` | yes | never | never |
| `isolated` | never | never | never |

Use `private` or `isolated` for confidential work. Set it when you add the
project, or change it later:

```bash
hpk project privacy <name> isolated
```

Moving a project towards `shared` is a loosening, so it needs `--yes` from a
person. Even then, nothing already in its wiki gets promoted on its own.

## How it is enforced

`privacy_guard` is a `pre_tool_call` hook with `fail_closed: true`: if the
guard breaks, Hermes denies the call. While an agent works inside a private or
isolated project, the guard:

- blocks tools that send content off the machine, including `send_message`,
  `discord`, `react_to_message`, and MCP tools whose names suggest sending,
  posting or mail
- blocks any write to `hpk-shared/`, including through `terminal` or
  `execute_code`
- for an isolated project, also blocks reads of `hpk-shared/`
- blocks an unknown tool that points at `hpk-shared/` rather than guessing

Reports for these projects are delivered locally only, to the Console. Cron
jobs created for them use local delivery whatever target you ask for, and the
Console refuses a notice target (`notify.to`) for them (docs/budgets.md).

Sessions that Hermes runs inside its gateway (Console chats, cron) are linked to
their project by hpk, so these rules apply to them too (docs/install.md).

## Local models only

`privacy.model_policy: local_only` keeps a project's content away from models
outside this machine. It works with any privacy mode. Set it with the switch on
the Console's privacy page or in `project.yaml`.

A model counts as local when its endpoint is loopback (`localhost`,
`*.localhost`, `127.0.0.0/8`, `::1`), or when an `lmstudio`, `ollama`,
`llamacpp` or `vllm` provider has no `base_url`, so it uses its local default.
Provider names alone are not trusted: an `lmstudio` route pointed at a remote
URL is remote. To count a machine on your own network, such as a GPU box, list
it in the project:

```yaml
privacy:
  model_policy: local_only
  local_hosts: [192.168.1.20]
```

hpk enforces the policy in three places:

1. **Before hpk starts model work.** The Console will not start a session,
   and `hpk cron install` will not create jobs, while the active Hermes
   profile's `model.base_url` is remote. The active profile is `$HERMES_HOME`
   or `<hermes home>/active_profile`. The Console's privacy page shows which
   profile it checked and where that profile sends requests.
2. **On every request inside Hermes.** The `model_route` hook runs on
   `pre_api_request` and `pre_auxiliary_call`, which cover titles and
   compression. It records the provider, model and host each session uses in
   `<hermes home>/hpk/routes/`.
3. **On every tool call.** `privacy_guard` refuses tools for a session whose
   last route is remote, and for any session that has sent a request to a
   remote host, even once. A session hpk has no record for is refused too,
   which is what happens when `model_route` is not installed. The guard fails
   closed.

What it cannot do:

- Hermes does not let a shell hook stop a model request, only a tool call. If
  you run plain `hermes` in a local-only project's folder on a remote model,
  your first message and the context files Hermes loads by itself (such as
  `AGENTS.md`) reach that model before any tool runs. hpk then refuses every
  tool, so the agent cannot read files, the wiki or anything else. Start
  sessions from the Console, or switch profile first (`hermes -p <local profile>`).
- A proxy on loopback, such as LiteLLM on `127.0.0.1:4000`, counts as local.
  hpk cannot see where the proxy forwards requests.

## Promoting a page

Promoting copies one page from a shared project's wiki to the shared wiki. It is
a red line, so a person always approves it. Before the copy is made, hpk:

- removes URLs, e-mail addresses, and file paths
- removes the project's name, its folder name, and the names on its glossary
  and people pages
- records an anonymous origin id (`project-<hash>`) instead of the project's
  name, so the page can be traced back or removed later

It errs on the side of removing too much. A person still reviews the draft.

Afterwards the original page and the shared copy are separate pages.

## What hpk never reads

hpk never opens `.env` files, key files or files that look like secrets: not
while adding a project, not while scanning, and not in the wiki. Adding a
project shows the token cost first and reads nothing until you agree.
