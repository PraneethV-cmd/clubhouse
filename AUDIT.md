# Clubhouse Audit

This pass reviewed the current Go codebase as a TUI/MCP coordination tool.

## Critical

- **Hook command paths break when the installed binary path contains spaces.**
  `internal/setup` writes hook commands as a single shell-like string and has a
  code comment acknowledging the assumption. This can break Codex hook execution
  on common macOS paths. Prefer a command representation that preserves argv or
  install a wrapper script at a stable no-space path.

- **HTTP clients have no timeout.** `internal/client` uses the default
  `http.Client`, so MCP calls and hooks can hang indefinitely if the coordinator
  accepts a connection and stalls. Hooks fail open only after the client returns.
  Add a short timeout for hook/MCP calls.

## High

- **The coordinator accepts unbounded JSON request bodies.** `decode` streams
  directly from `r.Body`. A bad local client can consume memory or disk via large
  lock/memory payloads. Wrap request bodies with `http.MaxBytesReader` and add
  field limits.

- **Snapshot writes are not atomic.** `save` writes directly to the snapshot
  path. A crash or interruption can leave a truncated JSON file. Write to a temp
  file, fsync, then rename.

- **File lock keys are only lightly normalized.** Absolute paths are converted
  relative to the hook-reported cwd, but path cleaning, symlink resolution, case
  behavior, and cross-checkout path equivalence are not fully handled. Normalize
  repo-relative paths consistently before locking.

- **Shell-write detection is heuristic.** The hook catches common write commands
  with substring matching, but misses many write forms and can false-positive on
  harmless strings. Prefer structured command policies where possible and keep
  the MCP/hook messaging clear when heuristics are used.

- **MCP heartbeat context never cancels.** `mcp.Run` creates
  `context.Background()` and starts a heartbeat goroutine that only stops when
  the process exits. Use a signal-aware context and cancel on server shutdown.

## Medium

- **Dashboard previously used a fixed 2x2 layout and byte-based truncation.**
  This caused weak resizing, poor hierarchy, broken wide-character handling, and
  no filtering. The dashboard now has responsive modes, ANSI-aware truncation,
  filter input, richer key help, gauges, and stronger visual hierarchy.

- **Invite and token persistence is local plaintext.** This is acceptable for a
  hackathon-style local tool but should be documented as a trust boundary. For
  broader use, restrict file permissions and consider token rotation/revocation.

- **Authentication is shared-token only.** Once a teammate redeems an invite,
  every client holds the same bearer token. That keeps setup simple but prevents
  per-member revocation and audit trails.

- **Server lifecycle is process-file based.** Stale PID files and address
  conflicts are handled minimally. A health check endpoint or explicit daemon
  status command would make operations safer.

- **Test coverage is concentrated around hooks, server behavior, and dashboard
  rendering.** Add tests for client timeouts, malformed payloads, snapshot
  corruption recovery, config setup idempotency, and MCP tool outputs.

## Low

- **UI text still mixes decorative symbols with content.** It is readable, but a
  future accessibility pass should verify screen reader behavior and plain-text
  fallbacks.

- **Room state is intentionally in memory.** This is pragmatic for small teams,
  but larger rooms will need pagination, event streaming, and durable storage.

- **README does not spell out threat model limits.** Add a short security model:
  trusted local teammates, localhost coordinator by default, shared bearer
  token, and fail-open hook behavior.
