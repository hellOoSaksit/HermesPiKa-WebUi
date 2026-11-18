# Contributing

Thank you for helping. hpk is source-available under MIT with the Commons
Clause (see LICENSE): you may use, change and share it, not sell it. By
contributing you agree that your contribution is under the same terms.

## Ground rules

- **No code from any Hermes web UI**, and no copied code, assets or names from
  other Hermes add-ons. Ideas are fine; write it yourself.
- **hpk talks to Hermes only through its official interfaces**: the `hermes` CLI,
  ACP (`hermes-acp`), hooks and skills. Never patch Hermes.
- **hpk never calls a model itself.** Model work goes through Hermes.
- **Guards fail closed.** If a red-line or privacy guard cannot decide, it denies.
- **Fictional data only.** Tests, docs and samples use made-up projects and
  people (`example.com`, `<hermes home>`). Never a real company, person, path or
  key. `node tools/leakcheck.mjs` checks this; put private words you want
  blocked in `.leakcheck-local` (git-ignored).

## Build and check

Requires Go (see go.mod) and Node 22 for the checks.

```sh
go test ./...
go vet ./...
gofmt -l .                     # must print nothing
node tools/leakcheck.mjs
node tools/check-console.mjs   # Console scripts parse; English and Thai match
go build -o dist/hpk ./cmd/hpk
```

The Console is embedded in the binary: rebuild after changing `assets/`. For
Console work without Hermes, `node tools/devserve.mjs 5173` serves it with demo
data.

## Pull requests

- One change per pull request, with tests for behaviour changes.
- Every user-visible string goes in both `assets/console/i18n/en.json` and
  `th.json`.
- Write docs and messages in plain words. Say what is not built yet instead of
  showing sample values.
- Note design decisions in the decision log at the end of docs/DESIGN.md.

## Releases

A maintainer tags `vX.Y.Z`. The release workflow tests, runs the leak check,
builds every platform with goreleaser (archives, `checksums.txt`, SBOMs) and
drafts a GitHub release. A person reviews the draft and publishes it.
