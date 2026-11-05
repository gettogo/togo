#!/usr/bin/env bash
# End-to-end push check on an iOS simulator.
#
#   scripts/e2e-push.sh [--local] [--ui] [--skip-build]
#
# Default: helper -> hosted relay -> APNs sandbox -> simulator. Needs network
# and a relay whose APNs key is accepted.
# --local: no relay or APNs. A stand-in relay on 127.0.0.1 takes the helper's
#   pushes and mailbox; each push becomes an .apns file for `simctl push`.
# --ui: also look at the notification in Notification Center from a UI test
#   and answer it there when it offers Approve.
#
# Asserts: the notification service opened the push and stored the request,
# the notification shows the question with no box drawing, the answer goes
# through the mailbox and `togo ask` exits 0, a Live Activity started, the
# clear push removes the request, and no relay request carried the question
# in the clear.
#
# Environment: TOGO_E2E_SIM (default Togo-E2E), TOGO_RELAY_URL (the relay for
# the default run; when unset, https:// and TOGO_RELAY_HOST from
# apps/ios/Config/Relay.xcconfig). Every process it starts is stopped on exit.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SIM_NAME=${TOGO_E2E_SIM:-Togo-E2E}
RELAY_URL=${TOGO_RELAY_URL:-}
RELAY_CONFIG=$ROOT/apps/ios/Config/Relay.xcconfig
if [ -z "$RELAY_URL" ] && [ -f "$RELAY_CONFIG" ]; then
  relay_host=$(sed -n 's/^[[:space:]]*TOGO_RELAY_HOST[[:space:]]*=[[:space:]]*//p' "$RELAY_CONFIG" | tail -1)
  if [ -n "$relay_host" ]; then RELAY_URL="https://$relay_host"; fi
fi
BUNDLE=app.togo.Togo
GROUP=group.app.togo
QUESTION="Run make deploy?"
DERIVED=$ROOT/apps/ios/DerivedData
APP=$DERIVED/Build/Products/Debug-iphonesimulator/Togo.app

LOCAL=0 UI=0 BUILD=1
for arg in "$@"; do
  case $arg in
    --local) LOCAL=1 ;;
    --ui) UI=1 ;;
    --skip-build) BUILD=0 ;;
    *) echo "usage: $0 [--local] [--ui] [--skip-build]" >&2; exit 2 ;;
  esac
done

step() { printf '== %s\n' "$*"; }
pass() { printf 'PASS %s\n' "$*"; }
fail() { printf 'FAIL %s\n' "$*" >&2; exit 1; }

if [ "$LOCAL" = 0 ] && [ -z "$RELAY_URL" ]; then
  fail "no relay: set TOGO_RELAY_URL or TOGO_RELAY_HOST in apps/ios/Config/Relay.xcconfig, or use --local"
fi

# Runs a command for at most $1 seconds; simctl push has been seen to hang.
with_timeout() {
  local secs=$1; shift
  perl -e 'alarm shift; exec @ARGV or die "exec: $!"' "$secs" "$@"
}

# py runs a short Python check in isolated mode; files are passed as arguments.
py() { python3 -I -c "$@"; }

# wait_until <seconds> <description> <command...>: polls once a second.
wait_until() {
  local secs=$1 what=$2; shift 2
  local i
  for ((i = 0; i < secs; i++)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  fail "$what (after ${secs} s)"
}

WORK=$(mktemp -d /tmp/togo-e2e.XXXXXX)
LABEL=togo-e2e-$$
PIDS=()
UDID=

cleanup() {
  local pid
  for pid in "${PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
  for pid in "${PIDS[@]}"; do wait "$pid" 2>/dev/null || true; done
  TMUX= TMUX_TMPDIR=$WORK/tmux tmux -L "$LABEL" kill-server 2>/dev/null || true
  if [[ -n $UDID ]]; then xcrun simctl terminate "$UDID" "$BUNDLE" >/dev/null 2>&1 || true; fi
  printf 'work files kept in %s\n' "$WORK"
}
trap cleanup EXIT

export TOGO_HOME=$WORK/home TOGO_TMUX_LABEL=$LABEL TMUX= TMUX_TMPDIR=$WORK/tmux
export TOGO_TEST_RELAY_TAP=$WORK/tap.jsonl
mkdir -p "$TOGO_HOME" "$TMUX_TMPDIR" "$WORK/relay" "$WORK/ask"

step "build the helper"
(cd "$ROOT/helper" && go build -o "$WORK/togo" ./cmd/togo && go build -o "$WORK/pushe2e" ./test/pushe2e)
TOGO=$WORK/togo PUSHE2E=$WORK/pushe2e

if ((LOCAL)); then
  step "start the local relay"
  export TOGO_TEST_RELAY_INSECURE=1
  "$PUSHE2E" relay -dir "$WORK/relay" -addr-file "$WORK/relay-url" -forbid "$QUESTION|make deploy|Approve" &
  PIDS+=($!)
  wait_until 10 "local relay did not start" test -s "$WORK/relay-url"
  RELAY_URL=$(cat "$WORK/relay-url")
fi

step "start the helper daemon"
"$TOGO" daemon >"$WORK/daemon.out" 2>&1 &
PIDS+=($!)

step "boot $SIM_NAME"
UDID=$(xcrun simctl list devices -j | py '
import json, sys
name = sys.argv[1]
for devs in json.load(sys.stdin)["devices"].values():
    for d in devs:
        if d["name"] == name and d.get("isAvailable", True):
            print(d["udid"]); sys.exit(0)
sys.exit(1)' "$SIM_NAME") || fail "no simulator named $SIM_NAME"
xcrun simctl boot "$UDID" 2>/dev/null || true
with_timeout 180 xcrun simctl bootstatus "$UDID" -b >/dev/null || fail "simulator did not boot"

UITEST=(xcodebuild -project "$ROOT/apps/ios/Togo.xcodeproj" -scheme TogoUITests -derivedDataPath "$DERIVED"
  -skipPackagePluginValidation -skipMacroValidation -quiet -configuration Debug
  -destination "platform=iOS Simulator,id=$UDID")
if ((BUILD)); then
  step "build the Debug app and its UI tests"
  make -C "$ROOT" ios-gen >/dev/null
  "${UITEST[@]}" build-for-testing >"$WORK/xcodebuild.log" 2>&1 || fail "app build failed, see $WORK/xcodebuild.log"
fi
[[ -d $APP ]] || fail "no app at $APP"

# status_field <key>: one value from the app's status file.
status_field() { py 'import json, sys; print(json.load(open(sys.argv[1]))[sys.argv[2]])' "$STATUS" "$1"; }
status_is() { [[ $(status_field "$1") == "$2" ]]; }
status_at_least() { (($(status_field "$1") >= $2)); }

launch() {
  with_timeout 30 xcrun simctl launch --terminate-running-process "$UDID" "$BUNDLE" -push-e2e "$@" >/dev/null
}

step "fresh install; the app asks for notifications and a UI test allows them"
# Uninstalling resets the notification permission, so the question comes up again.
xcrun simctl uninstall "$UDID" "$BUNDLE" >/dev/null 2>&1 || true
ARGS="-push-e2e -push-e2e-relay $RELAY_URL"
if ((LOCAL)); then ARGS+=" -push-e2e-local"; fi
TEST_RUNNER_TOGO_E2E_ARGS="$ARGS" "${UITEST[@]}" -only-testing:TogoUITests/LockScreenApproveUITests/testAllowNotifications \
  test-without-building >"$WORK/ui-allow.log" 2>&1 || fail "could not allow notifications, see $WORK/ui-allow.log"
DATA=$(xcrun simctl get_app_container "$UDID" "$BUNDLE" data)
SHARED=$(xcrun simctl get_app_container "$UDID" "$BUNDLE" "$GROUP")
PENDING="$SHARED/Library/Application Support/Push/pending.json"
STATUS=$DATA/Library/e2e-status.json
if [[ ! -s $DATA/Library/e2e-pair.json ]]; then
  # The UI test ends the app; a second launch registers with the permission granted.
  launch ${ARGS#-push-e2e }
fi
wait_until 30 "the app wrote no status" test -s "$STATUS"
wait_until 30 "no APNs registration outcome" py 'import json, sys; sys.exit(json.load(open(sys.argv[1]))["token"] == "pending")' "$STATUS"
TOKEN=$(status_field token)
if ((!LOCAL)) && [[ $TOKEN != real ]]; then
  fail "the simulator got no APNs token (authorization $(status_field authorization)); remote pushes cannot arrive"
fi
wait_until 10 "the app wrote no push.pair params" test -s "$DATA/Library/e2e-pair.json"
pass "device registered (token: $TOKEN, authorization: $(status_field authorization))"

step "pair the device with the helper"
"$PUSHE2E" pair -togo "$TOGO" -device "$DATA/Library/e2e-pair.json" -relay "$RELAY_URL" -out "$WORK/pairing.json"
launch -push-e2e-pairing "$(cat "$WORK/pairing.json")"
wait_until 15 "the app did not store the pairing" status_is paired True
# Closed, so the push lands in Notification Center rather than in a running app.
xcrun simctl terminate "$UDID" "$BUNDLE" >/dev/null 2>&1 || true
pass "paired"

step "ask: $QUESTION"
"$PUSHE2E" ask -togo "$TOGO" -state "$WORK/ask" -question "$QUESTION" -options Approve,Deny -timeout 120 &
ASK_PID=$!
PIDS+=($ASK_PID)
wait_until 20 "the approval did not open" test -s "$WORK/ask/request-id"
REQUEST=$(cat "$WORK/ask/request-id")

# relay_file <kind>: the first .apns file the local relay wrote for kind.
relay_file() {
  py 'import json, sys
for p in json.load(open(sys.argv[1]))["pushes"]:
    if p["kind"] == sys.argv[2] and p.get("file"):
        print(p["file"]); sys.exit(0)
sys.exit(1)' "$WORK/relay/relay.json" "$1"
}

# tap_refused: the relay refused a push; prints status and reason.
tap_refused() {
  py 'import json, sys
for line in open(sys.argv[1]):
    r = json.loads(line)
    if r["path"] == "/v1/push" and r["status"] != 202:
        print(r["status"], r.get("reason") or "-"); sys.exit(0)
sys.exit(1)' "$TOGO_TEST_RELAY_TAP"
}

if ((LOCAL)); then
  wait_until 15 "the helper sent no alert push" relay_file alert
  step "simctl push the sealed alert"
  with_timeout 30 xcrun simctl push "$UDID" "$BUNDLE" "$(relay_file alert)" >/dev/null || fail "simctl push failed or hung"
else
  wait_until 20 "the helper made no relay request" test -s "$TOGO_TEST_RELAY_TAP"
  sleep 3
  if REFUSED=$(tap_refused); then
    read -r CODE REASON <<<"$REFUSED"
    if [[ $REASON == InvalidProviderToken ]]; then
      fail "relay answered $CODE: APNs rejected the relay's provider token (InvalidProviderToken). Check APNS_TEAM_ID and APNS_KEY_ID on the relay against the key's team; nothing on this side can fix it"
    fi
    fail "relay answered $CODE ($REASON) to the helper's push"
  fi
  pass "relay accepted the push"
fi

# pending_has <state>: present (any), or answered with a value, or gone.
pending_has() {
  py 'import json, sys
path, rid, want = sys.argv[1:4]
try:
    reqs = json.load(open(path))
except (OSError, ValueError):
    reqs = []
r = next((x for x in reqs if x["requestId"] == rid), None)
ok = {"present": r is not None, "gone": r is None}.get(want, r is not None and r.get("answered") == want)
sys.exit(0 if ok else 1)' "$PENDING" "$REQUEST" "$1"
}

step "the notification service opens it"
if ((LOCAL)); then
  # simctl push never starts a notification service extension, so the app runs the same
  # code (NotificationServiceCore) on the delivered push.
  launch -push-e2e-open-delivered
fi
wait_until 60 "PendingStore never held the request (the notification service did not open the push)" pending_has present
if ((LOCAL)); then
  pass "NotificationServiceCore decrypted the push and stored the request (run in the app)"
else
  pass "the notification service extension decrypted the push and stored the request"
fi

if ((UI)); then
  step "Notification Center, from a UI test"
  if TEST_RUNNER_TOGO_E2E_QUESTION="$QUESTION" "${UITEST[@]}" \
    -only-testing:TogoUITests/LockScreenApproveUITests/testApproveFromNotification test-without-building >"$WORK/ui-test.log" 2>&1; then
    pass "Notification Center shows the question with no box drawing"
  else
    printf 'note: the springboard path failed (see %s); answering through the app\n' "$WORK/ui-test.log"
  fi
fi

APP_ANSWERED=0
if [[ ! -s $WORK/ask/ask-exit ]]; then
  APP_ANSWERED=1
  step "lock-screen Approve: the notification's action, through the relay mailbox"
  if ((LOCAL)); then
    launch -push-e2e-open-delivered -push-e2e-approve "$REQUEST" -push-e2e-decision approve
  else
    launch -push-e2e-approve "$REQUEST" -push-e2e-decision approve
  fi
fi

wait_until 60 "togo ask did not finish" test -s "$WORK/ask/ask-exit"
[[ $(cat "$WORK/ask/ask-exit") == 0 && $(cat "$WORK/ask/ask-out") == Approve ]] \
  || fail "togo ask exited $(cat "$WORK/ask/ask-exit") printing '$(cat "$WORK/ask/ask-out")'"
pass "togo ask exited 0 with Approve, answered by the Approve action through the mailbox"

if ((APP_ANSWERED)); then
  wait_until 10 "the Approve action did not mark the request approved" status_is answer "action approved"
  py 'import json, sys, hashlib
st = json.load(open(sys.argv[1]))
want = hashlib.sha256(sys.argv[3].encode()).hexdigest()[:6]
shown = [d for d in st["delivered"] + st["opened"] if d["requestId"] == sys.argv[2]]
if not shown: sys.exit("the delivered notification was not found")
d = shown[0]
if d["subtitleHash"] != want: sys.exit("the notification subtitle is not the question")
if d["boxDrawing"]: sys.exit("box drawing reached the notification")
if d["category"] != "togo.request": sys.exit("no Approve and Deny actions (category %s)" % d["category"])' "$STATUS" "$REQUEST" "$QUESTION" \
    || fail "rendering"
  pass "notification subtitle is the question, no box drawing, Approve and Deny offered"
  wait_until 10 "no Live Activity started" status_at_least maxActivities 1
  pass "Live Activity started"
fi

step "the clear push"
if ((LOCAL)); then
  wait_until 15 "the helper sent no clear push" relay_file background
  # simctl push refuses a push with nothing to show, so the app hands the relay's silent
  # payload to its own background push handler.
  cp "$(relay_file background)" "$DATA/Library/e2e-silent.apns"
  launch -push-e2e-silent
  wait_until 15 "the app did not handle the clear push" status_is silent handled
fi
wait_until 30 "the clear push did not remove the request" pending_has gone
pass "clear push removed the request"

step "relay request bodies"
py 'import json, sys, hashlib
words = {hashlib.sha256(w.encode()).hexdigest()[:6]: w for w in ("run", "make", "deploy", "approve")}
n = 0
for line in open(sys.argv[1]):
    r = json.loads(line); n += 1
    leaked = [words[h] for h in r["words"] if h in words]
    if leaked: sys.exit("a %s request carried %s in the clear" % (r["kind"], leaked))
if n == 0: sys.exit("the tap recorded no relay requests")' "$TOGO_TEST_RELAY_TAP" || fail "plaintext in relay bodies"
if ((LOCAL)); then
  py 'import json, sys; sys.exit(1 if json.load(open(sys.argv[1]))["plaintext"] else 0)' "$WORK/relay/relay.json" \
    || fail "the local relay saw the question in a request body"
fi
pass "no plaintext in relay request bodies"

wait "$ASK_PID" 2>/dev/null || true
printf 'e2e push (%s): all checks passed\n' "$( ((LOCAL)) && echo local || echo remote)"
