# Agent Workspace (`aw`)

A workspace-local command and package runtime for coding agents.

`aw` discovers the current repository, exposes existing package scripts, lets an agent persist useful commands, and installs namespaced command packages into the repository. It is a normal CLI, so Codex, Claude Code, Hermes, OpenCode, and humans use the same interface.

## Status

`v0.1.0` is a working local-first MVP:

- finds a workspace from nested directories;
- detects Git, GitHub Actions, pnpm/npm/yarn, Python, Go, Cargo, Taskfile, just, Cloudflare, Vercel, and Netlify markers;
- imports `package.json` scripts using the detected package manager;
- stores agent-authored commands in `.agent/workspace.yaml`;
- runs commands at the workspace root with argument forwarding and exit-code propagation;
- stamps `snapshot: git` commands and returns exit code `4` when the workspace changed during execution;
- installs local packages under `.agent/packages/<name>`;
- namespaces package commands as `<package>:<command>`;
- writes `workspace.lock` with a SHA-256 package digest and rejects modified installed packages.

Remote registries, webhooks, trigger matching, and deferred event inboxes are intentionally not in v0.1.

## Install

```bash
go install github.com/bkmashiro/agent-workspace/cmd/aw@latest
```

For local development:

```bash
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
```

`snapshot: git` hashes the starting Git state, including tracked diffs and untracked files. If the state changes before the command exits, `aw` reports the result as stale and exits `4` rather than treating an old green result as current verification.

It is a validity stamp, not an isolated worktree: v0.1 does not prevent concurrent writes while the command runs.

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

v0.1 accepts local directories only. The lockfile records the local source and content digest; immutable Git/registry sources are planned separately.

## Background use

`aw` deliberately remains a foreground process. The host harness owns background lifecycle and delivery:

```text
terminal(
  command="aw run full-test",
  background=true,
  notify_on_complete=true
)
```

This keeps process/session routing out of the CLI. Future delivery policies can choose between immediate wake, direct notification, and deferred next-turn injection without changing command packages.

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
