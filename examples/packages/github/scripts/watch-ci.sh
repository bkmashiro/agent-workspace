#!/bin/sh
set -eu

repo=""
sha=""
timeout=1800
poll=10

usage() {
  printf '%s\n' 'usage: watch-ci.sh [--repo owner/name] [--sha commit] [--timeout seconds] [--poll seconds]' >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo) [ "$#" -ge 2 ] || { usage; exit 2; }; repo=$2; shift 2 ;;
    --sha) [ "$#" -ge 2 ] || { usage; exit 2; }; sha=$2; shift 2 ;;
    --timeout) [ "$#" -ge 2 ] || { usage; exit 2; }; timeout=$2; shift 2 ;;
    --poll) [ "$#" -ge 2 ] || { usage; exit 2; }; poll=$2; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

case "$timeout:$poll" in
  *[!0-9:]*|:*) printf '%s\n' 'timeout and poll must be non-negative integers' >&2; exit 2 ;;
esac

command -v gh >/dev/null 2>&1 || { printf '%s\n' 'gh is required' >&2; exit 2; }
if [ -z "$repo" ]; then
  repo=$(gh repo view --json nameWithOwner --jq '.nameWithOwner')
fi
if [ -z "$sha" ]; then
  sha=$(git rev-parse HEAD)
fi

started=$(date +%s)
rows=""
while :; do
  rows=$(gh run list --repo "$repo" --commit "$sha" --limit 100 \
    --json databaseId,name,status,conclusion,url \
    --jq '.[] | [.databaseId,.name,.status,(.conclusion // ""),.url] | @tsv')

  if [ -n "$rows" ]; then
    # gh returns newest runs first. Keep only the newest attempt per workflow so
    # a superseded failed attempt does not poison a successful rerun.
    rows=$(printf '%s\n' "$rows" | awk -F '\t' '!seen[$2]++')
    pending=$(printf '%s\n' "$rows" | awk -F '\t' '$3 != "completed" { count++ } END { print count+0 }')
    if [ "$pending" -eq 0 ]; then
      break
    fi
  fi

  now=$(date +%s)
  if [ $((now - started)) -ge "$timeout" ]; then
    printf 'GITHUB_CI TIMEOUT\nrepository=%s\ncommit=%s\n' "$repo" "$sha"
    exit 2
  fi
  sleep "$poll"
done

total=$(printf '%s\n' "$rows" | awk 'NF { count++ } END { print count+0 }')
failed=$(printf '%s\n' "$rows" | awk -F '\t' '$4 != "success" && $4 != "neutral" && $4 != "skipped" { count++ } END { print count+0 }')

if [ "$failed" -eq 0 ]; then
  printf 'GITHUB_CI PASSED\nrepository=%s\ncommit=%s\nruns=%s\n' "$repo" "$sha" "$total"
  printf '%s\n' "$rows" | awk -F '\t' 'NF { printf "run=%s\t%s\t%s\n", $1, $2, $5 }'
  exit 0
fi

printf 'GITHUB_CI FAILED\nrepository=%s\ncommit=%s\nruns=%s\nfailed=%s\n' "$repo" "$sha" "$total" "$failed"
printf '%s\n' "$rows" | awk -F '\t' '$4 != "success" && $4 != "neutral" && $4 != "skipped" { printf "failed_run=%s\t%s\t%s\t%s\n", $1, $2, $4, $5 }'

log_file=$(mktemp "${TMPDIR:-/tmp}/aw-ci-logs.XXXXXX")
trap 'rm -f "$log_file"' EXIT HUP INT TERM
printf '%s\n' "$rows" | awk -F '\t' '$4 != "success" && $4 != "neutral" && $4 != "skipped" { print $1 }' |
while IFS= read -r run_id; do
  [ -n "$run_id" ] || continue
  printf '\n--- failed logs: run %s ---\n' "$run_id" >> "$log_file"
  gh run view "$run_id" --repo "$repo" --log-failed >> "$log_file" 2>&1 || true
done
printf '%s\n' 'failed_log_tail:'
tail -n 160 "$log_file"
exit 1
