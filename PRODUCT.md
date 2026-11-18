# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Plain HTML + CSS + JavaScript with no build step, served from the `hpk` Go binary (`hpk serve`) and embedded with `go:embed`. No frameworks, no node_modules, no third-party UI code. Decided by the owner (see `docs/DESIGN.md` §21–22).

## Users

Two primary audiences, weighted equally:

- **Solo developer** running Hermes Agent on their own machine, steering several agent sessions at once (a coordinator plus child sessions working in separate git worktrees). The Console sits open beside their editor and terminal all day.
- **Team lead on a shared server** where a team runs one Hermes instance. They watch several people's sessions, approve other people's red-line actions and wiki drafts, and resolve duplicate-work conflicts.

Both are technical Hermes users who already have Hermes installed.

## Product Purpose

HPK Console is the control surface of Hermes Project Kit (working name). It makes a project's agent work visible and governable:

- the tree of parent and child sessions
- who is working on what (the board and claims)
- what needs a human decision (approvals, red lines, wiki drafts, questions from child sessions)
- the project's accumulated knowledge (the wiki)

Success: nothing important happens without the human seeing it. The user never discovers duplicated work or lost knowledge after the fact, and decisions take seconds.

## Positioning

The Console governs agent work. Chatting with the agent is secondary. Humans sit at defined decision points, three modes (Manual / Ask / Auto) set how often the agent must ask, and no mode can cross a red line. Knowledge is captured to a per-project wiki with sources, approval and an audit trail. Everything is file-backed and inspectable.

## Operating Context

- Runs at `127.0.0.1` by default. On a team server it runs behind TLS and authentication (OIDC, passkey, or a header from a trusted proxy).
- Talks to Hermes Agent only through official interfaces: the `hermes` CLI, ACP, and the official dashboard API.
- Live updates arrive over SSE.
- Long-lived tab: it stays open for hours, so it must stay calm, scannable, and quiet until something needs attention.

## Capabilities and Constraints

v0.1 screens:

- overview
- projects and the add-project wizard (consent → privacy → scan → estimate → ingest → drafts → health → `project.yaml`)
- session tree and lanes
- light chat for sessions hpk created (stream, tool summaries, reply, queue, stop, permission requests)
- approvals inbox
- wiki browser and drafts review
- board, claims and duplicate-work alerts (🔴 duplicate, 🟠 file overlap, 🟡 related)
- modes, red lines, audit log and Auto digest
- scheduled jobs
- privacy and shared wiki
- team (roles and approvers)
- settings and health check (doctor)
- notifications

Constraints:

- Full chat for sessions hpk starts: new chat, search, pin, fork, edit and resend, slash commands, attachments, model and effort pickers, context ring, queue/steer/interrupt, child and summary cards, save to wiki. Built from scratch; Hermes WebUI is only a reference for features users expect. No voice, terminal or file browser in v0.1.
- Privacy modes are shared, private and isolated. Private projects never send content to off-machine channels.
- Interface languages are **English (default)** and **Thai**. All strings live in one dictionary so the user can switch at any time.
- Terminology to keep: session, parent/child, lane, claim, heartbeat, red line, draft, wiki, Auto/Ask/Manual, project.

## Brand Commitments

- The name is a working name ("Hermes Project Kit", short form "hpk").
- The product must not imitate Hermes WebUI or reuse its code, look or assets. It must not present itself as an official Nous Research product.

## Evidence on Hand

None yet. There are no real users, testimonials, metrics or screenshots. All data in the UI must be clearly fictional sample data, and no real project names, people or paths may appear.

## Product Principles

1. **Visible over magical.** Every agent action, decision and Auto approval can be seen and undone.
2. **Humans decide at named points.** Approvals are fast and batchable, and red lines are always unmistakable.
3. **Quiet until it matters.** Calm by default; urgent things such as red lines, duplicate work and blocked children rise clearly.
4. **Private by default.** Privacy state is always visible on project-scoped screens.
5. **Fast and light.** No build step and minimal payload. It must feel instant on a cheap server.

## Accessibility & Inclusion

- Keyboard operable throughout; approvals must be doable without a mouse.
- Meet WCAG 2.2 AA contrast in both themes.
- Thai and English text must both render and wrap correctly. Thai has no word spaces and needs adequate line-height.
- Respect `prefers-reduced-motion`.
