# `github` workspace package

Commands for asynchronous GitHub work using the authenticated `gh` CLI.

## Commands

### `github:ci`

Watches GitHub Actions workflow runs for a commit. It waits for runs to appear, coalesces their terminal states, and includes a bounded tail of failed logs.

```bash
aw run github:ci -- --timeout 1800 --poll 10
aw run github:ci -- --repo owner/repo --sha <commit>
```

Exit codes: `0` passed, `1` failed, `2` timeout/configuration error.

### `github:pr-review`

Watches the current pull request until its review decision changes or a review/comment arrives. It returns at most five recent reviews and five recent comments, truncating each body to 600 characters.

```bash
aw run github:pr-review -- --timeout 86400 --poll 30
aw run github:pr-review -- --repo owner/repo --number 42
```

Exit codes: `0` approved/new activity, `1` changes requested, `2` timeout/configuration error.

## Included trigger

Installing the package contributes this trigger:

```yaml
triggers:
  after-push:
    match: git push*
    run: ci
    delivery: defer
```

It is catalogued as `github:after-push` and resolves `run: ci` to `github:ci`. The package does not intercept shell commands; call `aw trigger fire --session <key> -- git push ...` from an agent or harness post-command hook after a successful push.

## Background execution

The package commands remain foreground processes. Start them with the host harness's tracked background-process primitive. In Hermes:

```text
terminal(
  command="aw run github:ci",
  background=true,
  notify_on_complete=true
)
```

Hermes currently resumes the Agent on either success or failure. A future delivery adapter can route success to a direct notification and only wake the Agent on failure.
