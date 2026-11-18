# Handoff: where hpk stands (2026-11-18)

For whoever continues this work (a person or an agent). Read this first, then
`CHANGELOG.md`, `PRODUCT.md` and `docs/docker.md`.

## Releases

| Version | State | What it is |
|---|---|---|
| v0.1.0 | published | First release. Still lists `hpk.exe` and the Windows zips: antivirus flags them as a trojan (heuristic). Delete those assets from the release page. |
| v0.1.1 | tag pushed, **draft not published** | No Windows binary; Windows runs hpk in Docker, solo mode; `install.ps1` sets it up. Publish the draft on the Releases page so `releases/latest` stops pointing at v0.1.0. |
| v0.1.2 | branch `fix/v0.1.2`, not merged, no tag | First-run fixes and a 100% Thai interface (see CHANGELOG). Merge to `main`, tag `v0.1.2`, publish. `compose.release.yaml` already names image `0.1.2`. |

The release workflow builds a **draft**; a person publishes it. Tests: `go test ./...`,
`node --test tools/*.test.mjs`, `node tools/check-console.mjs`, `node tools/leakcheck.mjs`.
No Go toolchain on the owner's PC: run Go in Docker
(`docker run --rm -v <repo>:/src -w /src golang:1.27 go test ./...`).

## How it runs on the owner's Windows PC

- `%USERPROFILE%\hpk`: `compose.release.yaml` + `.env` (`HPK_PROJECTS=C:/Users/<you>/Documents`).
  Container `hpk-hermes-hermes-1`, volume `hpk-hermes_hermes-data` (`/opt/data`, the Hermes home).
- Console: http://127.0.0.1:7878, solo mode. Start menu shortcut **HPK Console** opens a fresh
  one-time link (also: `docker compose -f compose.release.yaml exec hermes cat /opt/data/hpk/console-link`).
- Documents is `/projects` in the container (`/projects/MyProject/Roblox-Dev`, …).
- The old Hermes (containers `hermes-agent` + `hermes-webui`, volume `hermes-webui_hermes-home`)
  was imported on 2026-11-18: 3 Anthropic OAuth accounts, config, `.env`, profiles
  (`roblox-game-developer`), skills, memories, sessions. Backup of hpk's data before the import:
  `%USERPROFILE%\hpk\backups\hermes-data-before-import-*.tgz`.
  **Both Hermes copies now hold the same OAuth refresh tokens; run only one of them**, or a token
  refresh in one can sign the other out.

## Decisions the owner made

- Windows ships no `.exe` until it can be code-signed; Docker is the Windows path.
- Solo mode only for now; team mode is off (`HPK_MODE=team` brings it back).
- The Thai UI is 100% Thai: technical terms in Thai transliteration (glossary in the v0.1.2
  translation brief: เอเจนต์, เซสชัน, โปรเจกต์, วิกิ, โทเคน, คอมมิต, แบรนช์ …). Brand names (hpk,
  HPK Console), acronyms (AI, MCP, API), commands, paths and file names stay as they are.
- Users must never have to type a command: every task must be doable in the Console.
- Features must not copy Hermes WebUI's look or code (PRODUCT.md).

## Plan (essentials first; each version usable on its own)

1. **v0.2.0 ready to use without commands:** setup guidance on every page (what to do first,
   with a button); AI reply language setting (default = UI language); add AI accounts in the UI
   (API key and OAuth sign-in: hpk runs `hermes auth add` and relays the sign-in link);
   import an existing Hermes from the UI (scan, ask, copy; done by hand once for the owner).
2. **v0.3.0 general chat:** chat without a project; when a chat needs project work, offer to move
   it to a project chat (never forced); native-style folder picker, accept pasted Windows paths.
3. **v0.4.0 usage caps per account:** e.g. at most 70% of the 5-hour window and 80% of the weekly
   window, then switch to the next account. First check what usage data Hermes/providers expose.
4. **v0.5.0 data safety:** export everything (secrets included, after a clear risk warning);
   full / per-project / settings backups; import and restore (backup password so it works on
   another machine); tidy files when adding a project.
5. **v0.6.0 chat screen:** chat background; composer with saved prompts, voice, agent/profile,
   folder, model, effort, tools; side panel with background jobs, sub-tasks, changed files.
6. **v0.7.0 extensions:** skills (global and per project, on/off); MCP set up by the AI in a
   pop-out chat that fills hpk's own form (owner approves; secrets go to the vault); connector
   catalog; import MCP servers and skills from Claude Desktop / Claude Code (only missing ones).
7. **Later:** artifacts, memory across chats, writing styles, web search, project instructions,
   global hotkey, share/export chats.

## Known issues

- In the Claude desktop app's built-in browser the service worker fails to register
  ("unknown error fetching the script"); `sw.js` itself serves fine. Check in a normal browser.
- `/api/setup` answers 403 in Docker (it allows loopback callers only); harmless today because
  Hermes is already in the image, but first-run setup can't run there.
- CI on `windows-latest` failed on `main` before v0.1.2 because of the flaky test fixed in v0.1.2.
