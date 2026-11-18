# Security

hpk guards what agents may do (red lines, privacy modes, local-only models,
budgets) and, in team mode, who may use the Console. Please report problems in
those guards privately.

## Reporting

Use **Report a vulnerability** on this repository's Security tab (GitHub
private vulnerability reporting). Please do not open a public issue.

Include the hpk version (`hpk version`), the Hermes version, your OS, and the
steps to reproduce. Use fictional data in your report.

## What counts

- a guard that lets an action through when it should ask or deny, including
  when it fails
- a private or isolated project's content leaving the machine through hpk
- a way around sign-in or roles in team mode, or reading another person's chat
- hpk writing to or reading files it should not (Hermes' `.env`, secret files)
- leaked tokens, keys or personal data in logs, the Console or release files

Out of scope: problems in Hermes itself (report those to Hermes Agent), and
agents doing something allowed by the mode you chose.

## Design notes

- Guards fail closed. Hooks that only observe never block.
- Team tokens are stored only as SHA-256 hashes, in `team.yaml` (mode 0600).
- `hpk serve` listens beyond loopback only in team mode, with TLS (or behind a
  proxy that adds it).
- hpk never reads Hermes' `.env` and never sees a model API key.
