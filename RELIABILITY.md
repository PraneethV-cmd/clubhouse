# Clubhouse Reliability Notes

## Degradation Model

Clubhouse should not brick real work when one piece fails.

- MCP startup is fail-open: Codex can start even if the clubhouse server is down.
- MCP tool calls return actionable text when the room is unavailable.
- Hooks are fail-open on coordinator/network errors, but fail-closed on known lock conflicts.
- Lounge is observational: if it cannot reach the room, editing workflows should continue.
- Server snapshots are written atomically so a crash should not leave a partial JSON file.

## Lock Integrity

Locks are leases, not permanent ownership.

- A member keeps locks while it is heartbeating.
- Stop hooks release all locks held by that member.
- If a process dies, sleeps, or loses network, the member is pruned after the member TTL.
- When a member is pruned, all locks held by that member are released immediately.
- Orphan locks with no known member are released by the server sweep.
- Rejoining before timeout reuses the saved member id and keeps the member's locks.
- Rejoining after timeout creates a new member id; old locks have already been released.
- Mutating room calls from stale or unknown member ids are rejected, so a pruned
  session cannot create new ghost locks or memory entries.

This avoids the worst failure mode: a teammate leaves with a permanent lock.

## Consensual Data Integrity

The current model is intentionally conservative:

- The first active holder owns a file path lock.
- Other members are denied on known conflicts.
- A member can release only its own lock.
- Only currently joined members can lock files, release files, write memory, or
  record blocked events.
- Multi-file hook attempts roll back any locks acquired earlier in the same attempt when a later file conflicts.
- Conflict events are recorded in the activity feed.

Known limitation: if the coordinator is unreachable, hooks fail open so local work is not blocked.
That can allow two disconnected agents to edit the same file during a network partition. The
recovery story is then normal git conflict resolution. A stronger future version should add a
signed operation log or CRDT-like reconciliation for offline edits.

## Git Tree Visibility

Each heartbeat includes a lightweight git snapshot:

- branch
- dirty file count
- ahead/behind counts when available
- compact summary shown in the lounge and MCP status

This helps humans see whether a member is clean, dirty, ahead, or behind before coordinating work.

## Remaining Hard Problems

- Cross-checkout path equivalence with symlinks and generated files.
- Recovery after long offline partitions where both agents edited the same file.
- Authenticated remote hosting beyond localhost/LAN trust.
- Stronger audit trail for memory/lock decisions.
- User-facing repair flow for stale daemon, stale token, bad port, or broken Codex config.

`clubhouse check` now reports those repair targets as diagnostics, and
`clubhouse check --fix` rewrites the managed Codex hook/MCP block. The remaining
work is turning every warning into an automatic, consentful repair.
