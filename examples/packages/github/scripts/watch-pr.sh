#!/bin/sh
set -eu

repo=""
number=""
timeout=86400
poll=30

usage() {
  printf '%s\n' 'usage: watch-pr.sh [--repo owner/name] [--number N] [--timeout seconds] [--poll seconds]' >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo) [ "$#" -ge 2 ] || { usage; exit 2; }; repo=$2; shift 2 ;;
    --number) [ "$#" -ge 2 ] || { usage; exit 2; }; number=$2; shift 2 ;;
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
if [ -z "$number" ]; then
  number=$(gh pr view --repo "$repo" --json number --jq '.number')
fi

pr_state() {
  gh pr view "$number" --repo "$repo" --json number,url,reviewDecision,reviews,comments \
    --jq '[.number,.url,(.reviewDecision // ""),(.reviews | length),(.comments | length)] | @tsv'
}

baseline=$(pr_state)
baseline_decision=$(printf '%s\n' "$baseline" | awk -F '\t' '{ print $3 }')
baseline_reviews=$(printf '%s\n' "$baseline" | awk -F '\t' '{ print $4+0 }')
baseline_comments=$(printf '%s\n' "$baseline" | awk -F '\t' '{ print $5+0 }')
current=$baseline
started=$(date +%s)

case "$baseline_decision" in
  APPROVED|CHANGES_REQUESTED) ;;
  *)
    while :; do
      current=$(pr_state)
      decision=$(printf '%s\n' "$current" | awk -F '\t' '{ print $3 }')
      reviews=$(printf '%s\n' "$current" | awk -F '\t' '{ print $4+0 }')
      comments=$(printf '%s\n' "$current" | awk -F '\t' '{ print $5+0 }')
      if [ "$decision" != "$baseline_decision" ] || [ "$reviews" -gt "$baseline_reviews" ] || [ "$comments" -gt "$baseline_comments" ]; then
        break
      fi
      now=$(date +%s)
      if [ $((now - started)) -ge "$timeout" ]; then
        printf 'GITHUB_PR TIMEOUT\nrepository=%s\npr=%s\n' "$repo" "$number"
        exit 2
      fi
      sleep "$poll"
    done
    ;;
esac

url=$(printf '%s\n' "$current" | awk -F '\t' '{ print $2 }')
decision=$(printf '%s\n' "$current" | awk -F '\t' '{ print $3 }')
reviews=$(printf '%s\n' "$current" | awk -F '\t' '{ print $4+0 }')
comments=$(printf '%s\n' "$current" | awk -F '\t' '{ print $5+0 }')
status=${decision:-UPDATED}
printf 'GITHUB_PR %s\nrepository=%s\npr=%s\nurl=%s\nreviews=%s\ncomments=%s\n' \
  "$status" "$repo" "$number" "$url" "$reviews" "$comments"

gh pr view "$number" --repo "$repo" --json reviews,comments --jq '
  ([.reviews[] | "review: \(.author.login) [\(.state)] \(.body | gsub("[\\r\\n]+"; " ") | .[0:600])"] |
    if length > 5 then .[-5:] else . end)[],
  ([.comments[] | "comment: \(.author.login) \(.body | gsub("[\\r\\n]+"; " ") | .[0:600])"] |
    if length > 5 then .[-5:] else . end)[]
'

if [ "$decision" = "CHANGES_REQUESTED" ]; then
  exit 1
fi
exit 0
