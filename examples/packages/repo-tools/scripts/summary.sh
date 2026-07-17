#!/bin/sh
set -eu

printf 'root=%s\n' "$AW_WORKSPACE_ROOT"
if git -C "$AW_WORKSPACE_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'branch=%s\n' "$(git -C "$AW_WORKSPACE_ROOT" branch --show-current)"
  printf 'head=%s\n' "$(git -C "$AW_WORKSPACE_ROOT" rev-parse --short HEAD)"
  printf 'changes=%s\n' "$(git -C "$AW_WORKSPACE_ROOT" status --short | wc -l | tr -d ' ')"
else
  printf 'git=false\n'
fi
