#!/usr/bin/env bash
# Records a real agent prompt as a detector fixture, without attaching.
# Usage: scripts/capture-prompt.sh <agent> <name> <workdir> <prompt...> [-- <command...>]
#   agent:   claude | codex | opencode | omp | generic
#   name:    fixture name, lowercase letters, digits and dashes
#   workdir: name of the throwaway git repo created under
#            $TMPDIR/togo-capture/ (a Makefile, notes.txt and todo.txt)
#   prompt:  typed into the agent once its UI has settled; may be empty
#   command: the agent argv, run in the throwaway repo; defaults to <agent>
# The agent runs in a private tmux label at 120x40. Once `togo detect`
# matches the pane, the screen is saved to
# helper/internal/detect/testdata/<agent>/<name>.txt with $HOME, $USER, the
# host name and the throwaway path scrubbed, and <name>.json is written from
# `togo detect`. The prompt is then dismissed with Escape and the label
# killed; nothing is ever approved. Nothing is written when no prompt shows
# up within 180 seconds. Read the capture before committing it.
# CAPTURE_OUT overrides the testdata directory, for dry runs; CAPTURE_WAIT
# overrides the 180 second wait. CAPTURE_KEEP=1 saves the last screen with
# match false when nothing is detected, so a detector gap gets a fixture.
set -euo pipefail

die() { echo "capture-prompt: $*" >&2; exit 1; }

[ $# -ge 3 ] || die "usage: capture-prompt.sh <agent> <name> <workdir> <prompt...> [-- <command...>]"
agent="$1" name="$2" work="$3"
shift 3
case "$agent" in claude | codex | opencode | omp | generic) ;; *) die "unknown agent: $agent" ;; esac
[[ "$name" =~ ^[a-z0-9][a-z0-9-]{0,63}$ ]] || die "invalid name: $name"
[[ "$work" =~ ^[a-z0-9][a-z0-9-]{0,63}$ ]] || die "invalid workdir: $work"

prompt=()
while [ $# -gt 0 ] && [ "$1" != "--" ]; do
  prompt+=("$1")
  shift
done
[ $# -gt 0 ] && shift
if [ $# -eq 0 ]; then
  [ "$agent" != generic ] || die "generic needs a command after --"
  set -- "$agent"
fi
text="${prompt[*]:-}"

tmux="$(command -v tmux)" || die "tmux not found"
command -v "$1" >/dev/null || die "command not found: $1"
command -v go >/dev/null || die "go not found"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dir="${CAPTURE_OUT:-$root/helper/internal/detect/testdata}/$agent"
txt="$dir/$name.txt" json="$dir/$name.json"
[ ! -e "$txt" ] && [ ! -e "$json" ] || die "fixture $agent/$name already exists"

base="${TMPDIR:-/tmp}"
base="${base%/}/togo-capture"
mkdir -p "$base"
togo="$base/togo"
(cd "$root/helper" && go build -o "$togo" ./cmd/togo)

repo="$base/$work"
[ ! -e "$repo" ] || die "workdir $repo already exists; pick another name"
mkdir -p "$repo"
printf 'all:\n\t@echo built\n\nclean:\n\t@echo cleaned\n' >"$repo/Makefile"
printf 'Meeting notes\n' >"$repo/notes.txt"
printf 'Buy milk\n' >"$repo/todo.txt"
git -C "$repo" init -q
git -C "$repo" add -A
git -C "$repo" -c user.name=togo -c user.email=togo@example.invalid commit -qm init

label="togo-capture-$$"
tm() { "$tmux" -L "$label" "$@"; }
cleanup() {
  tm send-keys -t capture Escape 2>/dev/null || true
  sleep 0.5
  tm kill-server 2>/dev/null || true
}
trap cleanup EXIT

tm -f /dev/null new-session -d -s capture -x 120 -y 40 -c "$repo" -- "$@"
tm set-option -t capture remain-on-exit on >/dev/null
screen() { tm capture-pane -p -t capture; }

# Wait for the agent UI: non-empty and unchanged for 2 seconds.
last="" stable=0
for _ in $(seq 1 120); do
  sleep 0.5
  now="$(screen)"
  if [ -n "${now//[[:space:]]/}" ] && [ "$now" = "$last" ]; then
    stable=$((stable + 1))
    [ "$stable" -ge 4 ] && break
  else
    stable=0
  fi
  last="$now"
done
[ "$stable" -ge 4 ] || { screen >&2; die "agent UI did not settle within 60 seconds"; }

if [ -n "$text" ]; then
  # Startup dialogs (folder trust, updates, sign-in) would take the typed
  # Enter as an answer; stop instead of typing into them.
  if screen | "$togo" detect >/dev/null ||
    screen | grep -Eiq '(❯|›|▸) *[0-9]+\.|enter to confirm|press enter|update available|trust (this|the files)|sign in'; then
    screen >&2
    die "a dialog is on screen before typing; dismissed without answering"
  fi
  tm send-keys -t capture -l "$text"
  sleep 0.5
  tm send-keys -t capture Enter
fi

# Wait for a prompt that stays put for one more second.
fingerprint() { "$togo" detect 2>/dev/null | grep '"fingerprint"' || true; }
found=""
for _ in $(seq 1 "${CAPTURE_WAIT:-180}"); do
  sleep 1
  [ "$(tm display-message -p -t capture '#{pane_dead}')" != 1 ] ||
    { screen >&2; die "the agent exited before a prompt showed up"; }
  fp="$(screen | fingerprint)"
  [ -n "$fp" ] || continue
  sleep 1
  again="$(screen)"
  if [ "$(printf '%s\n' "$again" | fingerprint)" = "$fp" ]; then
    found="$again"
    break
  fi
done
if [ -z "$found" ]; then
  [ "${CAPTURE_KEEP:-}" = 1 ] || { screen >&2; die "no prompt detected in time; nothing written"; }
  found="$(screen)"
  echo "capture-prompt: no prompt detected; keeping the screen with match false" >&2
fi

scrub() {
  local real
  real="$(cd "$repo" && pwd -P)"
  SCRUB_REAL="$real" SCRUB_REPO="$repo" SCRUB_WORK="~/$work" \
    SCRUB_HOME="$HOME" SCRUB_USER="$USER" SCRUB_HOST="$(hostname -s)" \
    perl -pe '
      s/\Q$ENV{SCRUB_REAL}\E/$ENV{SCRUB_WORK}/g;
      s/\Q$ENV{SCRUB_REPO}\E/$ENV{SCRUB_WORK}/g;
      s/\Q$ENV{SCRUB_HOME}\E/~/g;
      s/\Q$ENV{SCRUB_USER}\E/user/g;
      s/\Q$ENV{SCRUB_HOST}\E/mac/g;
    '
}

mkdir -p "$dir"
printf '%s\n' "$found" | scrub >"$txt"
version="$("$1" --version 2>/dev/null | head -n 1 | tr -d '"\\' || true)"
detected="$("$togo" detect <"$txt" || true)"
{
  printf '{\n  "source": "real",\n  "agentVersion": "%s",\n' "${version:-unknown}"
  printf '%s\n' "$detected" | tail -n +2
} >"$json"
if [ -n "${CAPTURE_KEEP:-}" ] || grep -q '"match": true' "$json"; then :; else
  echo "capture-prompt: matched before scrubbing but not after; check $txt" >&2
fi
echo "saved $txt"
echo "read it for secrets and real paths before committing"
