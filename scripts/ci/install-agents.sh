#!/bin/sh
# Installs the coding agents the nightly checks run on a hosted runner: the ones that work with
# only an API key, each at its newest release. An agent that fails to install is reported and the
# rest carry on; the checks then run on what is there. Prints one "<id> <version>" line per
# installed agent on stdout, everything else on stderr.
#
#   scripts/ci/install-agents.sh                 # every agent in the table
#   scripts/ci/install-agents.sh opencode pi     # only these
#
# Agents tied to an account sign-in (Codex, Copilot CLI, Cursor CLI, Amp, Auggie, Kimi Code,
# Droid, Kiro CLI) are not installed here: a hosted runner cannot sign in to them.
set -u

# id | installer | package or URL | command
table='
opencode|npm|opencode-ai|opencode
omp|npm|@oh-my-pi/pi-coding-agent|omp
pi|npm|@mariozechner/pi-coding-agent|pi
aider|uv|aider-chat|aider
goose|script|https://github.com/block/goose/releases/download/stable/download_cli.sh|goose
crush|npm|@charmland/crush|crush
cline|npm|cline|cline
kilo|npm|@kilocode/cli|kilo
'

want="$*"
failed=""
for row in $table; do
    id=${row%%|*}; rest=${row#*|}
    how=${rest%%|*}; rest=${rest#*|}
    pkg=${rest%%|*}; cmd=${rest#*|}
    if [ -n "$want" ]; then
        case " $want " in *" $id "*) ;; *) continue ;; esac
    fi
    echo "== $id" >&2
    case "$how" in
        npm) npm install --global --no-fund --no-audit "$pkg" >&2 ;;
        uv) uv tool install --quiet "$pkg" >&2 ;;
        script) curl -fsSL "$pkg" | CONFIGURE=false sh >&2 ;;
    esac
    status=$?
    if [ $status -ne 0 ] || ! command -v "$cmd" >/dev/null 2>&1; then
        echo "   $id did not install (status $status)" >&2
        failed="$failed $id"
        continue
    fi
    # The first thing in the version output that looks like a version number.
    version=$("$cmd" --version 2>/dev/null | sed -n 's/.*\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9a-z.+-]*\).*/\1/p' | head -n 1)
    echo "$id ${version:-unknown}"
done
[ -n "$failed" ] && echo "not installed:$failed" >&2
exit 0
