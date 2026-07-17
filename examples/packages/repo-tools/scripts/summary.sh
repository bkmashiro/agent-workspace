#!/bin/sh
set -eu

printf 'root=%s\n' "$AW_WORKSPACE_ROOT"
if git -C "$AW_WORKSPACE_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'branch=%s\n' "$(git -C "$AW_WORKSPACE_ROOT" branch --show-current)"
  if head=$(git -C "$AW_WORKSPACE_ROOT" rev-parse --short HEAD 2>/dev/null); then
    printf 'head=%s\n' "$head"
  else
    printf 'head=unborn\n'
  fi
  printf 'changes=%s\n' "$(git -C "$AW_WORKSPACE_ROOT" status --short | wc -l | tr -d ' ')"
else
  printf 'git=false\n'
fi
