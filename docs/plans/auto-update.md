# Plan: updates from inside hpk

Status: planned, not built. To be developed later inside hpk itself.

## Today (0.1.0)

There is no automatic update. A person updates by doing the install again:
- double-click a newer `hpk.exe`, or
- re-run `install.ps1` / `install.sh`, or
- for Docker, change the image tag in `compose.release.yaml` and run `docker compose pull && docker compose up -d`.

Self-install stops the running hpk, replaces `<hpk data>/bin/hpk` and starts the new one. Data, projects, wikis and the vault stay.

## What to build

1. **Check:** HPK Console checks `https://api.github.com/repos/hellOoSaksit/HermesPiKa-WebUi/releases/latest` once a day, and on demand from Settings.
   - Skip drafts. Skip prereleases unless the person opts in.
   - Compare with `version.Version` (semver). A dev build (`dev`) never offers updates.
2. **Tell:** show a banner "hpk 0.1.x is available", with the release notes (the CHANGELOG section) and an **Update** button.
   - No silent updates. An update restarts hpk, so a chat or job that is running would be cut.
   - Offer "after the current work finishes".
3. **Update** (button, or `hpk update` in a terminal):
   - Download the archive for this OS/arch plus `checksums.txt` from the release.
   - Verify SHA-256 (the same rules as the install scripts: exact file name, refuse on mismatch or a missing line).
   - Then run the existing self-install path: stop, replace, start. Windows: the move-aside of a busy exe already exists in `internal/selfinstall`.
4. **Refresh the copy Hermes uses:** `hpk install` copies hpk to `<hermes home>/hpk/bin` for the hooks, and under isolation the AI runs that copy. After an update, refresh it too, by re-running the install step when the manifest exists.
   - Without this, the Console and the hooks run different versions.
   - Also: the setup status (`internal/serve/setupapi.go`) reports "installed" whenever that file exists. It should compare versions and offer a refresh.
5. **Docker:** no in-container self-update. The Console shows the new tag and the two commands.
6. **Rollback:** keep the previous binary as `hpk.prev[.exe]` for one version. `hpk update --rollback` swaps it back.
7. **Settings:** "Check for updates" on/off (default on), "Include pre-releases" (default off), and the last-checked time. The check sends nothing but a plain GET, with no identifiers.

## Safety

- Download only from the project's GitHub releases over https. Verify checksums before running anything.
- Under isolation, the update runs as the person, never as the AI account. The AI-side hook keeps refusing `hpk update` as a tool call, like `hpk isolate`.
- Not signed yet. Code signing (Windows Authenticode, macOS notarization) would remove the SmartScreen and Gatekeeper prompts, and would let the updater check a signature as well as the checksum.

## Tests

- A fake release server, like `tools/install-scripts.test.mjs`. Cover: newer, same, older, prerelease, missing asset, bad checksum, no network.
- The refresh of the Hermes-home copy.
- Rollback.
