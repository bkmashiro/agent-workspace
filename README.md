# Agent Workspace (`aw`)

A workspace-local command and package runtime for coding agents.

`aw` discovers the current repository, exposes existing package scripts, lets an agent persist useful commands, and installs namespaced command packages into the repository. It is a normal CLI, so Codex, Claude Code, Hermes, OpenCode, and humans use the same interface.

## Status

`v0.2.0` is a working local-first MVP:

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
- ships fixture-tested GitHub CI and PR-review watcher packages.

Hosted registries, webhooks, trigger matching, and deferred event inboxes are intentionally not in v0.2.

## Install

From a local checkout:

```bash
go install ./cmd/aw
# or
go build -o bin/aw ./cmd/aw
```

No hosted release or package registry entry exists yet.

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

It is a validity stamp, not an isolated worktree: v0.2 does not prevent concurrent writes while the command runs.

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
aw install https://github.com/owner/repository.git \\
  --ref v0.2.0 \\
  --subdir packages/example
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
