#!/bin/sh
# Checks scripts/ci/install-agents.sh against stand-ins for npm, uv and curl: no network, nothing
# installed for real. Run from anywhere: scripts/ci/install-agents-test.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
bin="$tmp/bin"
mkdir -p "$bin"

# An installed agent is a script that prints a version. npm fails for one package on purpose.
agent() { printf '#!/bin/sh\necho "%s"\n' "$2" > "$bin/$1"; chmod +x "$bin/$1"; }
cat > "$bin/npm" <<EOF
#!/bin/sh
for a in "\$@"; do pkg=\$a; done
case "\$pkg" in
    opencode-ai) printf '#!/bin/sh\necho "1.19.6"\n' > "$bin/opencode"; chmod +x "$bin/opencode" ;;
    @mariozechner/pi-coding-agent) printf '#!/bin/sh\necho "pi 0.73.1 (build x)"\n' > "$bin/pi"; chmod +x "$bin/pi" ;;
    @charmland/crush) exit 1 ;;
    *) exit 0 ;;   # "installs" but leaves no command behind
esac
EOF
cat > "$bin/uv" <<EOF
#!/bin/sh
printf '#!/bin/sh\necho "aider 0.99.2"\n' > "$bin/aider"; chmod +x "$bin/aider"
EOF
printf '#!/bin/sh\nexit 22\n' > "$bin/curl"
chmod +x "$bin/npm" "$bin/uv" "$bin/curl"

fail() { echo "FAIL: $1" >&2; exit 1; }

out=$(PATH="$bin:/usr/bin:/bin" sh "$here/install-agents.sh" 2>"$tmp/err") || fail "the installer exited non-zero when some agents failed"
expected='opencode 1.19.6
pi 0.73.1
aider 0.99.2'
[ "$out" = "$expected" ] || fail "version list was:
$out"
grep -q "crush did not install" "$tmp/err" || fail "a failed npm install was not reported"
grep -q "omp did not install" "$tmp/err" || fail "an install that leaves no command was not reported"
grep -q "goose did not install" "$tmp/err" || fail "a failed download was not reported"
grep -q "not installed: omp goose crush cline kilo" "$tmp/err" || fail "the summary of failures was: $(tail -1 "$tmp/err")"

# Only the agents named.
out=$(PATH="$bin:/usr/bin:/bin" sh "$here/install-agents.sh" pi 2>/dev/null)
[ "$out" = "pi 0.73.1" ] || fail "with one agent named: $out"

echo "install-agents: ok"
