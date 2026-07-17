# Agent Workspace (`aw`)

[![CI](https://github.com/bkmashiro/agent-workspace/actions/workflows/ci.yml/badge.svg)](https://github.com/bkmashiro/agent-workspace/actions/workflows/ci.yml)

A workspace-local command and package runtime for coding agents.

`aw` discovers the current repository, exposes existing package scripts, lets an agent persist useful commands, and installs namespaced command packages into the repository. It is a normal CLI, so Codex, Claude Code, Hermes, OpenCode, and humans use the same interface.

## Status

`v0.3.0` is a working local-first MVP:

- finds a workspace from nested directories;
- detects Git, GitHub Actions, pnpm/npm/yarn, Python, Go, Cargo, Taskfile, just, Cloudflare, Vercel, and Netlify markers;
- imports `package.json` scripts using the detected package manager;
- stores agent-authored commands in `.agent/workspace.yaml`;
- runs commands at the workspace root with argument forwarding and exit-code propagation;
- stamps `snapshot: git` commands and returns exit code `4` when the workspace changed during execution;
- installs local directories or a package subdirectory from a fixed Git ref under `.agent/packages/<name>`;
- records the resolved Git commit plus a SHA-256 package digest;
- namespaces package commands as `<package>:<command>`;
- rejects modified installed packages;
- ships fixture-tested GitHub CI and PR-review watcher packages;
- persists command-pattern triggers in workspace or package manifests;
- routes trigger results through `wake` or a session-isolated deferred inbox.

Hosted registries, webhooks, automatic harness hooks, and remote notification adapters are intentionally not in v0.3.

## Install

```bash
go install github.com/bkmashiro/agent-workspace/cmd/aw@latest
```

From a local checkout:

```bash
go install ./cmd/aw
# or
go build -o bin/aw ./cmd/aw
```

## Quick start

```bash
cd your-project
aw init
aw inspect
aw list

aw add verify --description "Run repository verification" -- \
  pnpm lint '&&' pnpm typecheck '&&' pnpm test

aw run verify
```

A project that already has `package.json` scripts does not need `aw init` before discovery:

```bash
aw list
# pnpm:build   package.json script [package.json]
# pnpm:test    package.json script [package.json]

aw run pnpm:test
```

Pass extra arguments after `--`:

```bash
aw run pnpm:test -- --runInBand
```

## Workspace manifest

```yaml
version: 1
commands:
  verify:
    run: go test ./... && go vet ./...
    description: Run repository verification
  full-test:
    run: pnpm test
    description: Run the full test suite
    snapshot: git
triggers:
  ci-after-push:
    match: git push*
    run: github:ci
    delivery: defer
```

`snapshot: git` hashes the starting Git state, including tracked diffs and untracked files. If the state changes before the command exits, `aw` reports the result as stale and exits `4` rather than treating an old green result as current verification.

It is a validity stamp, not an isolated worktree: v0.3 does not prevent concurrent writes while the command runs.

## Packages

A package is a directory containing `package.yaml` and optional scripts/assets:

```yaml
name: repo-tools
version: 0.1.0
commands:
  summary:
    run: "$AW_PACKAGE_DIR/scripts/summary.sh"
    description: Print a compact repository summary
```

Install it into the current workspace:

```bash
aw install ./examples/packages/repo-tools
aw run repo-tools:summary
```

Package commands receive:

```text
AW_WORKSPACE_ROOT  absolute workspace root
AW_PACKAGE_DIR     installed package directory
AW_COMMAND         resolved command name
```

Packages are copied into `.agent/packages/`, symlinks are rejected, and every `list` or `run` verifies the installed content against `.agent/workspace.lock`.

Install from a Git repository and pin the resolved commit in the lockfile:

```bash
aw install https://github.com/bkmashiro/agent-workspace.git \\
  --ref main \\
  --subdir examples/packages/github
```

`--ref` accepts a commit, tag, or branch fetch ref, but the lockfile always records the resolved commit SHA and content digest. Hosted package discovery and version resolution remain out of scope.

## GitHub watcher package

The included `github` package requires an authenticated `gh` CLI:

```bash
aw install ./examples/packages/github

# Watch all workflow runs for the current HEAD.
aw run github:ci

# Wait for a review decision or new review/comment on the current PR.
aw run github:pr-review
```

Both commands accept bounded polling options:

```bash
aw run github:ci -- --timeout 1800 --poll 10
aw run github:pr-review -- --timeout 86400 --poll 30
```

`github:ci` waits for runs to appear, coalesces workflow state, and emits at most the last 160 lines of failed logs. It exits `0` when all runs succeed, `1` on a terminal failure, and `2` on timeout/configuration errors.

`github:pr-review` returns recent bounded review/comment bodies. It exits `1` for `CHANGES_REQUESTED`, `0` for approval or other new activity, and `2` on timeout/configuration errors.

The package also contributes `github:after-push`, a deferred `git push*` trigger that runs `github:ci`. Installing a package makes its triggers discoverable but does not install a global shell hook.

## Triggers and deferred inbox

A trigger maps an observed shell command to an `aw` command:

```bash
aw trigger add ci-after-push \\
  --match 'git push*' \\
  --run github:ci \\
  --delivery defer

aw trigger list --json
aw trigger match --json -- git push origin main
```

`aw` does not intercept arbitrary shell processes. A harness hook, wrapper, or agent fires the trigger only after the observed command succeeds:

```bash
aw trigger fire --session "$SESSION_KEY" -- git push origin main
```

`fire` executes every matching trigger command. Delivery controls what happens afterward:

- `wake`: stream the command result and propagate its exit code. A harness can use its normal completion notification to resume the agent.
- `defer`: store the bounded result in the session inbox and return `0`, even when the watched command failed. This avoids an immediate LLM call.

For a long-running deferred watcher, launch `aw trigger fire` with the host's tracked background primitive and disable completion notification. Read it on the next existing turn:

```bash
aw inbox list --session "$SESSION_KEY" --json
aw inbox drain --session "$SESSION_KEY" --json
```

`drain` returns and consumes the selected session's pending events. Runtime state lives outside the repository under `$AW_STATE_HOME`, `$XDG_STATE_HOME/aw`, or `~/.local/state/aw`; the workspace only stores trigger definitions. `AW_SESSION_ID` or `HERMES_SESSION_ID` can provide the default key, otherwise pass `--session` explicitly.

This release provides the deterministic CLI substrate. Automatic `pre_llm_call` injection still requires a small harness adapter; `aw` does not claim that a queued event is injected by itself.

## Background use

Commands remain foreground processes. The host harness owns lifecycle:

```text
terminal(
  command="aw run full-test",
  background=true,
  notify_on_complete=true
)
```

Use completion notification for `wake`; omit it for `defer` and drain the inbox during the next already-occurring turn.

## Agent discovery

A repository only needs a compact instruction:

```markdown
Run `aw list --json` to discover workspace commands before guessing workflows.
```

The full package documentation does not need to occupy the model context.

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/aw
```

## License

MIT
