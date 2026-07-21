# Clubhouse Demo Runbook

## Preflight

Run this once before presenting:

```sh
cd /Volumes/abyss-brick/projects/openai-hax
go build -o /tmp/clubhouse ./cmd/clubhouse
install /tmp/clubhouse "$HOME/.local/bin/clubhouse"
clubhouse check --fix
scripts/hackathon-smoke.sh
```

Expected proof points:

- Fresh host starts on `127.0.0.1:8787`.
- Alice and Bob enter from the invite.
- Collision smoke prints `alice got 200, bob got 409`.
- Hook smoke prints `bob was blocked by alice's lock`.
- MCP smoke prints `serverInfo=clubhouse`.
- Lounge smoke prints `rendered PEOPLE/LOCKS`.
- Menu smoke writes `/tmp/clubhouse-hackathon-smoke/menu/index.md`.

## Live Demo

Open the lounge:

```sh
cd /tmp/clubhouse-hackathon-smoke/host
clubhouse lounge
```

Tell the story:

1. Teams will run many Codex agents in the same repo.
2. Agents collide on files and duplicate work.
3. Clubhouse gives them a shared room: presence, hard locks, memory, and MCP tools.
4. The second writer is denied before it corrupts the work.
5. `clubhouse menu` turns the repo into a Markdown knowledge graph.

In Codex, after restart, ask:

```text
who's in the clubhouse?
```

For a non-interactive MCP smoke, after `scripts/hackathon-smoke.sh`:

```sh
scripts/codex-mcp-smoke.sh
```

This uses Codex's automation bypass flags because `codex exec` can auto-cancel MCP
tool approval prompts in non-interactive mode. The live presentation path should
still be interactive Codex.

## Backup Commands

If the port is stale:

```sh
clubhouse close --port
scripts/hackathon-smoke.sh
```

If MCP startup warnings appear:

```sh
clubhouse check --fix
```

Then restart Codex.

If the lounge looks empty:

```sh
cd /tmp/clubhouse-hackathon-smoke/alice && clubhouse lounge
cd /tmp/clubhouse-hackathon-smoke/bob && clubhouse lounge
```

If you need the invite again:

```sh
cd /tmp/clubhouse-hackathon-smoke/host
clubhouse invite
```
