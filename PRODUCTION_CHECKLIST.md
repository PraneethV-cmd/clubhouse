# Clubhouse Production Checklist

Use this file as the single production tracker. Mark an item complete only after
the verification line passes.

## P0 - Ship Blockers

- [ ] Clean release state
  - Status: pending
  - Owner:
  - Verification: `git status --short` contains only intentional source/release files.
  - Notes: Remove runtime files, generated Hugo output, release tarballs, and local binaries before the release commit.

- [ ] Real fresh-install test
  - Status: in progress
  - Owner:
  - Verification: On a clean user/machine, `curl -fsSL https://raw.githubusercontent.com/PraneethV-cmd/clubhouse/main/install.sh | sh` installs `clubhouse`, then `clubhouse host` succeeds.
  - Notes: Requires a published GitHub Release with tarballs and `checksums.txt`; latest local installer smoke uses the renamed `PraneethV-cmd/clubhouse` release assets.

- [ ] Codex hook compatibility
  - Status: in progress
  - Owner:
  - Verification: Real Codex session has silent allow path, deny path blocks an edit, and Stop hook releases locks.
  - Notes: Silent allow is implemented; multi-file denied edits now roll back locks claimed earlier in the same hook run. `scripts/hackathon-smoke.sh` verifies actual `clubhouse hook pre` denial and Stop unlock behavior. Confirm one live Codex UI denial if time permits.

- [ ] End-to-end multi-agent collision test
  - Status: in progress
  - Owner:
  - Verification: Host plus two Codex users editing the same file causes one lock and one clear denial.
  - Notes: `scripts/hackathon-smoke.sh` starts a fresh local room, verifies a real coordinator lock collision, and verifies hook-level denial.

- [ ] MCP production smoke
  - Status: in progress
  - Owner:
  - Verification: Codex can call `status`, `presence`, `whos_editing`, `lock`, `unlock`, `remember`, and `recall`.
  - Notes: Direct MCP handler smoke test covers `presence`, `whos_editing`, `lock`, `unlock`, `remember`, and `recall`; MCP startup is fail-open; `scripts/hackathon-smoke.sh` verifies direct stdio initialize returns `serverInfo=clubhouse`; `scripts/codex-mcp-smoke.sh` verifies `codex exec` can call `clubhouse/status` with automation bypass flags.

- [ ] Installer/release pipeline
  - Status: pending
  - Owner:
  - Verification: GitHub Actions publishes macOS/Linux amd64/arm64 archives and `checksums.txt`; `install.sh` verifies checksums.
  - Notes: Confirm missing assets fail clearly.

- [ ] TUI visual QA
  - Status: in progress
  - Owner:
  - Verification: `clubhouse lounge` renders correctly at 80x24, 100x30, wide desktop, light/dark terminal, and `NO_COLOR=1`.
  - Notes: `scripts/hackathon-smoke.sh` runs a 100x30 pty lounge render and verifies `PEOPLE`/`LOCKS`; render tests cover 80x24 and the below-minimum `80x24` warning. Still needs quick manual wide/light/dark/NO_COLOR glance.

## P1 - Production Beta Requirements

- [ ] Onboarding polish
  - Status: in progress
  - Owner:
  - Verification: `clubhouse host` handles host/enter paths, cancel, missing invite, and next-step copy cleanly.
  - Notes: Host flow now separates member/display name from clubhouse room name.

- [ ] `clubhouse check --fix` hardening
  - Status: in progress
  - Owner:
  - Verification: Detects stale daemon, dead port, bad binary path, old MCP process, missing PATH, missing Codex, and broken release install.
  - Notes: `clubhouse check` now reports Codex config problems separately from environment warnings: missing Codex, missing/PATH-mismatched Clubhouse binary, stale daemon pid, existing MCP process, missing room config/token, unreachable configured server, and missing configured binary paths. `--fix` rewrites the managed Codex block; automatic repair for every runtime warning remains pending.

- [ ] Daemon/server robustness
  - Status: in progress
  - Owner:
  - Verification: Graceful shutdown, bounded request bodies, atomic snapshots, and port-conflict tests pass.
  - Notes: Stale member sessions now rejoin cleanly before lounge/MCP/hooks operate; request bodies are bounded; snapshots are written atomically; blocked feed events persist immediately; member timeout releases held locks; orphan locks are swept; unknown/stale member ids cannot create new locks, memory, or blocked events.

- [ ] Path and lock correctness
  - Status: in progress
  - Owner:
  - Verification: Symlinks, absolute paths, repo-relative paths, shell writes, apply_patch moves/deletes, and cross-checkout equivalence are tested.
  - Notes: Config/session/token writes now recover when a demo pane's current directory was deleted and recreated; multi-file hook conflicts release earlier claims; covered by `internal/config` and `internal/hook` tests.

- [ ] Docs/troubleshooting
  - Status: in progress
  - Owner:
  - Verification: README covers happy path, localhost demo, hook failures, stale daemon cleanup, reinstall/update, and known limitations.
  - Notes: Added `DEMO_RUNBOOK.md` with preflight, live demo flow, and stale-port/MCP/lounging fallback commands. Added `RELIABILITY.md` for degradation, lock leases, stale member rejection, git tree visibility, network partitions, and remaining risks.

- [ ] Test coverage
  - Status: in progress
  - Owner:
  - Verification: CLI aliases, installer dry-run, MCP handlers, setup/check config, server TTL/timeout, and TUI render tests are in place.
  - Notes: Added stale-session, deleted-cwd recovery, multi-file hook rollback, server body-limit/persistence, stale-member lock rejection, orphan-lock cleanup, minimum-window TUI, setup diagnostic, and MCP handler smoke tests.

- [ ] Code menu knowledge graph
  - Status: in progress
  - Owner:
  - Verification: `clubhouse menu` generates `menu/index.md`, package pages, symbol pages, and a Mermaid graph on a real repo.
  - Notes: First pass uses deterministic Go AST scanning; optional `clubhouse menu --codex` runs `codex exec` to enrich only generated menu files.

## P2 - Public Launch Polish

- [ ] Nix packaging
  - Status: pending
  - Owner:
  - Verification: `nix run` and `nix build` produce a real packaged binary, not only a dev wrapper.
  - Notes:

- [ ] Public website
  - Status: pending
  - Owner:
  - Verification: Website has production copy, screenshots/GIF, install variants, release links, and troubleshooting.
  - Notes:

- [ ] Security hardening
  - Status: pending
  - Owner:
  - Verification: Trust model, token storage, remote hosting expectations, invite expiry, and non-localhost warnings are documented.
  - Notes:

- [ ] Distribution extras
  - Status: pending
  - Owner:
  - Verification: Homebrew tap, signed/notarized macOS binaries, SBOM/provenance, and update path are decided.
  - Notes:
