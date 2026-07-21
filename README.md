# 🏠 clubhouse

**A multiplayer coordination layer for teams running Codex agents in the same repo.**

As agentic coding takes off, a team won't have one dev with one agent — they'll have
many devs running many Codex agents against the same codebase. Those agents collide:
two of them editing `auth.ts` at once, redoing each other's work, with nobody able to
see who's doing what.

Clubhouse is the coordination layer. Everyone joins a shared **clubhouse** for the repo,
runs plain `codex`, and the agents become room-aware:

- **Hard file locks** — a Codex `PreToolUse` hook blocks any agent from editing a file
  another agent is already editing. Not advisory: the edit is *denied*, and the agent is
  told who holds it.
- **Live presence** — see who's in the room and what each agent is working on.
- **Agent-facing tools** — via MCP, the agent itself can check `whos_editing`, claim a
  file, and read/write **shared project memory** ("what's already been tried").
- **A live dashboard** — `clubhouse lounge`.
- **A code menu** — `clubhouse menu` maps packages, types, functions, methods, and
  dependencies into portable Markdown under `menu/`.

Built on Codex's own extension points: **hooks**, an **MCP server**, and a **skill**.

## Install

For most users:

```sh
curl -fsSL https://raw.githubusercontent.com/PraneethV-cmd/clubhouse/main/install.sh | sh
clubhouse host
```

For Go users:

```sh
make install                     # go install ./cmd/clubhouse
clubhouse host
```

For Nix users working from this repo:

```sh
nix develop
nix run
```

Requires Codex installed locally. The curl installer downloads a release binary;
Go is only required for source installs.

## Use it

**Host the clubhouse** (runs in the background):
```sh
clubhouse host                  # guided setup
clubhouse open                  # prints an invite link; stop it with `clubhouse close`
```

The host flow asks for two separate names: your **member name**, shown next to
your presence, and the **clubhouse name**, shown as the room title in the lounge.

**Each teammate**, in their checkout of the repo:
```sh
clubhouse enter rc://join/...   # paste the invite link
clubhouse setup                 # wires the hook + MCP + skill into Codex
codex                           # just run Codex normally
```

Now inside Codex, ask *"who's in the clubhouse?"* to see presence + the share link.
When two agents reach for the same file, the second is **blocked**.

**Watch the room live** (any pane):
```sh
clubhouse lounge
```

**Map the codebase**:
```sh
clubhouse menu                 # writes menu/index.md, menu/graph.md, packages/, symbols/
clubhouse menu --tests         # include Go test files too
clubhouse menu --codex         # optional Codex pass to enrich the generated summaries
```

## Local multi-user demo

Create a localhost host, alice, and bob without touching your current repo state:

```sh
scripts/localhost-demo.sh
```

To open the three lounge windows automatically on macOS:

```sh
CLUBHOUSE_OPEN_TERMINAL=1 scripts/localhost-demo.sh
```

Cleanup:

```sh
scripts/localhost-demo.sh reset
```

## Hackathon smoke test

Run this before the demo. It resets the localhost clubhouse, starts a fresh host,
joins Alice and Bob, proves a lock collision, and generates a menu graph:

```sh
scripts/hackathon-smoke.sh
```

For the presentation flow and backup commands, see `DEMO_RUNBOOK.md`.

Reliability and failure-mode notes live in `RELIABILITY.md`.

## Commands

| Command | What |
|---|---|
| `clubhouse host` | guided setup for hosting or entering a clubhouse |
| `clubhouse open [--addr] [--rotate 10m]` | start the coordinator in the background |
| `clubhouse close` | stop it |
| `clubhouse enter <link>` | join a clubhouse |
| `clubhouse setup` / `unsetup` | wire / unwire Codex |
| `clubhouse check [--fix]` | diagnose or repair Codex hook/MCP registration |
| `clubhouse invite` | print the current (rotating) invite link |
| `clubhouse lounge` (or bare `clubhouse`) | live dashboard |
| `clubhouse menu [--out menu] [--tests] [--codex]` | generate a Markdown knowledge graph for the repo |

Compatibility aliases remain: `serve` = `open`, `join` = `enter`, and
`watch` = `lounge`.

Config lives in `.clubhouse/config.txt` (plain `key = value`; leave `name` blank for a
random handle). Invite links carry a **rotating join code** that expires, so a leaked
link doesn't grant access forever — members already in keep a stable token.

## How it works

- `clubhouse open` is a small HTTP coordinator holding room state (members, locks, memory).
- `clubhouse setup` registers a **PreToolUse hook** (`clubhouse hook pre`) and an **MCP
  server** (`clubhouse mcp`) in `~/.codex`, plus a `clubhouse` skill.
- `clubhouse check --fix` removes duplicate Clubhouse hook/MCP blocks and writes one
  clean managed Codex registration.
- `clubhouse check` also warns about local runtime issues such as missing PATH entries,
  missing Codex, stale daemon pid files, existing MCP processes, missing room tokens,
  and unreachable configured servers.
- On every file edit, the hook asks the coordinator to claim the file — first agent wins,
  everyone else is denied (exit 2 + reason, which Codex surfaces to the agent).
- The MCP server gives the agent `status` / `presence` / `whos_editing` / `lock` /
  `unlock` / `remember` / `recall` tools.
- The lounge expects at least an `80x24` terminal. Below that it shows a compact
  resize message instead of a broken dashboard.

## Layout

```
cmd/clubhouse     CLI entrypoint + background daemon
internal/server   coordinator (room state, rotating join codes, snapshots)
internal/client   coordinator client
internal/hook     Codex PreToolUse / Stop hooks (the hard lock)
internal/mcp      MCP server (agent-facing tools)
internal/setup    Codex wiring (hook + MCP + skill), reversible
internal/dash     live dashboard (Bubble Tea)
internal/menu     Markdown knowledge graph generator
internal/room     shared types + invite links
internal/config   .clubhouse config, token, session
internal/names    random handles
```

Go · Charm Bubble Tea + Lip Gloss + Bubbles · Codex hooks, MCP, and skills.
