# MCP connections and background jobs

MCP servers give the agent tools from other programs (Blender, Roblox Studio,
Godot, a browser) and from web services. **Hermes owns the setting**: it lives
under `mcp_servers` in the profile's `config.yaml`. hpk shows it in the Console
(Settings → MCP) and changes it only through `hermes mcp add / remove / test`.

## Two places, two rules

| Where hpk runs | Web MCP (a URL) | Local program (a command) |
|---|---|---|
| Your own machine | yes | yes: Blender, Roblox Studio, Godot, Playwright, a folder, … |
| Team server (team mode, or `HPK_MCP_URL_ONLY=1`) | yes, `https` only | **no**: a server does not start programs for the team |

On a server a URL on the server itself (`localhost`, `127.x`, `::1`) is refused
too: it would be a program running on the server. MCPs that only work on a
person's machine (Figma desktop, Blender, Roblox Studio) are connected from hpk
on that person's machine. An MCP already in a server's `config.yaml` that breaks
the rule is shown in red and cannot be tested from the Console.

Adding, testing and removing are for owners (`team.Admin`). Members see the
list and the usage.

## How it is done

- **Add**: hpk runs `hermes mcp add <name> --url …` or `--command … --args …`
  with `HERMES_HOME` set to the active profile and answers Hermes' questions on
  stdin. Hermes connects, lists the tools and saves the entry. If the program
  does not answer (Blender is closed, Studio is not open), Hermes still saves it
  **switched off**; **Test** in the Console adds it again, which probes it and
  switches it on.
- **Tokens** (web MCPs): passed to Hermes on stdin; Hermes stores them in its
  `.env` as `MCP_<NAME>_API_KEY` and writes only `Bearer ${MCP_<NAME>_API_KEY}`
  in `config.yaml`. hpk never stores, logs or returns a token, and only accepts
  one over HTTPS or from a browser on the same machine. On Windows Hermes reads
  tokens from the keyboard only, so a web MCP with a token is added in a
  terminal there.
- **Environment variables** of local programs are passed with `--env` and kept
  by Hermes in `config.yaml`; the Console shows their names, never the values.
- Open chats keep their tools; new chats get the new set (or `/reload-mcp`).

## What each MCP is used by

hpk's `privacy_guard` hook runs before every tool call. For an MCP tool
(`mcp__<server>__<tool>`) it appends one line to `<hermes home>/hpk/mcp-use.jsonl`:
time, server, tool, Hermes session, project and the project's repository folder,
and whether the call was blocked. **Never the tool's input or output.** The file
rolls over at 1 MB (one old copy, `.1`).

The MCP details in the Console show calls per day (14 days), the projects and
repositories, tools and chats that used it, the skills whose `SKILL.md` names
its tools (`mcp__<server>__…`), and what of it runs right now.

## Background jobs

Settings → MCP and Projects → Jobs list what runs for the agent now:

- commands Hermes started in the background (its `processes.json`);
- local MCP programs, found among this machine's processes by their command.

**Stop** ends the process and the processes it started. hpk stops a process only
if it is still the one listed (same id and start time), so a reused process id
is never hit. Processes in Hermes' sandbox (`pid_scope: sandbox`) are shown but
stopped from their chat. The Hermes gateway itself is never listed. Stopping is
for owners.
