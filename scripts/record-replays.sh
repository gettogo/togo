#!/usr/bin/env bash
# Records replay fixtures (protocol/v1/replays/*.jsonl) from real agent
# sessions on this Mac, through the installed helper. Every session starts
# muted, uses tiny prompts and cheap models, and is killed afterwards.
# Nothing here signs in, types credentials or changes an agent's config.
#
#   scripts/record-replays.sh                 # the whole matrix
#   scripts/record-replays.sh claude.approve  # fixtures whose name matches
#
# Agents that are not installed are skipped. A scenario that does not finish
# still writes its fixture with "complete": false and a note in the header.
set -uo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/protocol/v1/replays"
# Inside the checkout, a folder the agents already trust (no first-run dialog).
scratch="${TOGO_REPLAY_SCRATCH:-$root/.replay-scratch}"
bin="$(mktemp -d)/togo-record"
(cd "$root/helper" && go build -o "$bin" ./cmd/togo-record) || exit 1
mkdir -p "$out" "$scratch"

# name | agent | scenario | args (comma-separated) | model to switch to
matrix=(
  "claude.turn|claude|turn|--model,haiku|"
  "claude.approve|claude|approve|--model,haiku,--permission-mode,default|"
  "claude.deny|claude|deny|--model,haiku,--permission-mode,default|"
  "claude.question|claude|question|--model,haiku,--permission-mode,default|"
  "claude.interrupt|claude|interrupt|--model,haiku|"
  "claude.model|claude|model|--model,haiku|sonnet"
  "claude.subagent|claude|subagent|--model,haiku|"
  "claude.error|claude|error|--model,no-such-model-replay|"
  "codex.turn|codex|turn|-m,gpt-5-mini|"
  "codex.approve|codex|approve|-m,gpt-5-mini,-a,untrusted|"
  "gemini.signin|gemini|signin|-m,gemini-2.5-flash-lite|"
  "gemini.turn|gemini|turn|-m,gemini-2.5-flash-lite|"
  "gemini.approve|gemini|approve|-m,gemini-2.5-flash-lite|"
  "gemini.deny|gemini|deny|-m,gemini-2.5-flash-lite|"
  "gemini.interrupt|gemini|interrupt|-m,gemini-2.5-flash-lite|"
  "gemini.model|gemini|model|-m,gemini-2.5-flash-lite|gemini-2.5-flash"
  "opencode.turn|opencode|turn||"
  "opencode.interrupt|opencode|interrupt||"
  "omp.turn|omp|turn||"
  "omp.interrupt|omp|interrupt||"
)
for agent in copilot droid amp cursor goose crush kilo vibe auggie cline kimi; do
  matrix+=("$agent.turn|$agent|turn||")
done
# aider writes its history at the git root unless told to leave git alone.
matrix+=("aider.turn|aider|turn|--no-git,--no-gitignore|")

filters=("$@")
for row in "${matrix[@]}"; do
  IFS='|' read -r name agent scenario args model <<<"$row"
  if ((${#filters[@]})); then hit=; for f in "${filters[@]}"; do [[ "$name" == *"$f"* ]] && hit=1; done; [[ -z "$hit" ]] && continue; fi
  program="$agent"; [[ "$agent" == cursor ]] && program=cursor-agent
  command -v "$program" >/dev/null || { echo "skip $name: $program not installed"; continue; }
  mkdir -p "$scratch/$agent"
  echo "== $name"
  "$bin" -agent "$agent" -scenario "$scenario" -args="$args" -model "$model" \
    -cwd "$scratch/$agent" -out "$out/$name.jsonl" -timeout 150s || echo "   ($name incomplete)"
  rm -f "$scratch/$agent/replay-probe.txt"
done
rm -rf "$(dirname "$bin")"
