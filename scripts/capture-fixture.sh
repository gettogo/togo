#!/usr/bin/env bash
# Captures a real prompt screen for the detector fixtures.
# Usage: scripts/capture-fixture.sh <agent> <name> [command...]
#   agent: claude | codex | opencode | omp | generic
#   name:  fixture name, lowercase letters, digits and dashes
#   command defaults to the agent name ($SHELL for generic).
# The agent runs in a private tmux label so the user's sessions and the
# helper's own label are never touched. Drive it to the prompt you want,
# then detach (prefix d); the visible pane is saved to
# helper/internal/detect/testdata/<agent>/<name>.txt with a .json
# expectation skeleton to fill in. Review the capture for secrets before
# committing it.
set -euo pipefail

die() { echo "capture-fixture: $*" >&2; exit 1; }

[ $# -ge 2 ] || die "usage: capture-fixture.sh <agent> <name> [command...]"
agent="$1" name="$2"
shift 2
case "$agent" in claude | codex | opencode | omp | generic) ;; *) die "unknown agent: $agent" ;; esac
[[ "$name" =~ ^[a-z0-9][a-z0-9-]{0,63}$ ]] || die "invalid name: $name"
if [ $# -eq 0 ]; then
  if [ "$agent" = generic ]; then set -- "${SHELL:-/bin/sh}"; else set -- "$agent"; fi
fi
command -v tmux >/dev/null || die "tmux not found"
command -v "$1" >/dev/null || die "command not found: $1"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dir="$root/helper/internal/detect/testdata/$agent"
txt="$dir/$name.txt" json="$dir/$name.json"
[ ! -e "$txt" ] && [ ! -e "$json" ] || die "fixture $agent/$name already exists"

label="togo-capture-$$"
trap 'tmux -L "$label" kill-server 2>/dev/null || true' EXIT
tmux -L "$label" -f /dev/null new-session -d -s capture -x 120 -y 40 -- "$@"
tmux -L "$label" set-option -t capture remain-on-exit on >/dev/null
tmux -L "$label" attach-session -t capture

mkdir -p "$dir"
tmux -L "$label" capture-pane -p -t capture >"$txt"
version="$("$1" --version 2>/dev/null | head -n 1 | tr -d '"\\' || true)"
cat >"$json" <<EOF
{
  "source": "real",
  "agentVersion": "${version:-unknown}",
  "match": true,
  "kind": "",
  "title": "",
  "options": [],
  "keys": {}
}
EOF
echo "saved $txt"
echo "fill in $json, then run: cd helper && go test ./internal/detect/"
