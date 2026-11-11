# Cases for scripts/install-test.sh (sourced; bash). Each case gets a fresh
# directory $C, its own release tree served at $BASE, a sandbox PATH built
# from the tools it names, and an environment of only HOME, PATH and the
# test-mode variables. Refusals must leave the install directory byte for
# byte as it was.

CASES=(
  installs_fresh upgrades_existing_atomically
  refuses_tampered_archive refuses_tampered_sums refuses_other_key
  refuses_old_version_comment refuses_rewritten_comment refuses_truncated_download refuses_missing_sig
  refuses_http_redirect refuses_env_override_on_remote_url
  refuses_unknown_arch rosetta_picks_arm64
  uses_shasum_without_sha256sum uses_wget_without_curl uses_busybox_wget
  refuses_http_redirect_with_wget refuses_symlinked_togo runs_nothing_when_truncated
  openssl_path_without_minisign minisign_path codesign_path_on_darwin
  refuses_without_any_verifier installs_to_togo_bin_by_default
  restarts_daemon_when_service_installed installs_service_by_default runs_from_a_pipe rejects_bad_usage
)

BASE_TOOLS=(awk cat chmod cp cut grep gzip head mkdir mktemp mv od pwd rm sed tail tar tr uname wc sysctl ln readlink)

fail_msg() { FAILED="${FAILED}      $*"$'\n'; }
skip_case() { SKIPPED="$*"; }

# setup_case NAME: fresh $C and served tree $WWW holding v1.0.0 as latest.
setup_case() {
  C="$W/cases/$SH_TAG/$1"
  CASE_ID="$SH_TAG-$1"
  WWW="$W/www/$CASE_ID"
  BASE="http://127.0.0.1:$PORT/$CASE_ID"
  mkdir -p "$C/home" "$C/bin" "$WWW/download"
  : >"$C/out"
  : >"$C/err"
  printf 'v1.0.0\n' >"$WWW/latest"
  publish v1.0.0
  ENV=(TOGO_INSTALL_BASE_URL="$BASE" TOGO_INSTALL_TEST_KEY="$TEST_KEY")
}

publish() { cp -R "$W/rel/$1" "$WWW/download/$1"; }
asset() { printf '%s\n' "$WWW/download/v1.0.0/togo_1.0.0_$1.tar.gz"; }
host_archive() { asset "${HOST_OS}_$HOST_ARCH"; }

# link_tool NAME [PATH]: put NAME into the sandbox PATH.
link_tool() {
  local p="${2:-$(command -v "$1" || true)}"
  [ -n "$p" ] && ln -sf "$p" "$C/bin/$1"
}
# tools [-default] NAME...: base tools plus NAMEs; "default" adds curl,
# a sha256 tool and OpenSSL 3.
tools() {
  local t
  for t in "${BASE_TOOLS[@]}"; do link_tool "$t"; done
  for t in "$@"; do
    case "$t" in
      default)
        link_tool curl
        link_tool sha256sum || link_tool shasum
        link_tool openssl "$OPENSSL"
        ;;
      openssl) link_tool openssl "$OPENSSL" ;;
      *) link_tool "$t" ;;
    esac
  done
}
# link_libressl: macOS ships LibreSSL as /usr/bin/openssl, which cannot check
# the signature; put it on PATH where it exists, as on a stock Mac.
link_libressl() {
  case "$(/usr/bin/openssl version 2>/dev/null)" in LibreSSL*) link_tool openssl /usr/bin/openssl ;; esac
}
# wrap NAME: a logging wrapper around the real NAME in $C/NAME.log.
wrap() {
  local real
  real="$(command -v "$1")"
  printf '#!/bin/sh\nprintf "%%s\\n" "$*" >>"%s"\nexec "%s" "$@"\n' "$C/$1.log" "$real" >"$C/bin/.$1"
  install_script "$1"
}
# install_script NAME: move $C/bin/.NAME over NAME. Never write through a
# symlink, which would point at the host's real tool.
install_script() {
  chmod +x "$C/bin/.$1"
  mv -f "$C/bin/.$1" "$C/bin/$1"
}
# fake NAME SCRIPT-BODY: a /bin/sh script on the sandbox PATH.
fake() {
  printf '#!/bin/sh\n%s\n' "$2" >"$C/bin/.$1"
  install_script "$1"
}

# fakeable NAME...: succeed when the shell under test runs a PATH fake for each
# NAME. A busybox built with standalone-shell applets runs its own uname, wget,
# sha256sum... and never looks at PATH, so those cases cannot be faked there;
# the case skips itself, saying why.
fakeable() {
  local n out
  mkdir -p "$C/probe"
  for n in "$@"; do
    printf '#!/bin/sh\necho fake\n' >"$C/probe/$n"
    chmod +x "$C/probe/$n"
    out="$(env -i PATH="$C/probe" "${SH[@]}" -c "$n" 2>/dev/null </dev/null || true)"
    [ "$out" = fake ] || { skip_case "$SH_TAG runs its built-in $n, so a PATH fake is not used"; return 1; }
  done
}

# run_install [VAR=value ...] -- ARGS...: run install.sh in the sandbox.
run_install() {
  local extra=()
  while [ $# -gt 0 ] && [ "$1" != "--" ]; do extra+=("$1"); shift; done
  [ "${1:-}" != "--" ] || shift
  set +e
  if [ "${PIPED:-}" = 1 ]; then
    env -i HOME="$C/home" PATH="$C/bin" ${ENV[@]+"${ENV[@]}"} ${extra[@]+"${extra[@]}"} \
      "${SH[@]}" -s -- "$@" >"$C/out" 2>"$C/err" <"$INSTALL_SH"
  else
    env -i HOME="$C/home" PATH="$C/bin" ${ENV[@]+"${ENV[@]}"} ${extra[@]+"${extra[@]}"} \
      "${SH[@]}" "$INSTALL_SH" "$@" >"$C/out" 2>"$C/err" </dev/null
  fi
  CODE=$?
  set -e
}

snapshot() { (cd "$1" && find . -print | LC_ALL=C sort && find . -type f -exec cksum {} + | LC_ALL=C sort) 2>&1; }
# prepare_dir: an install directory already holding togo v0.9.0.
prepare_dir() {
  mkdir -p "$C/dir"
  cp "$W/bins/v0.9.0_${HOST_OS}_$HOST_ARCH" "$C/dir/togo"
  BEFORE="$(snapshot "$C/dir")"
}

expect_code() { [ "$CODE" = "$1" ] || fail_msg "exit $CODE, want $1"; }
expect_err() { grep -qF -- "$1" "$C/err" || fail_msg "stderr lacks: $1"; }
expect_out() { grep -qF -- "$1" "$C/out" || fail_msg "stdout lacks: $1"; }
expect_no_out() { ! grep -qF -- "$1" "$C/out" "$C/err" || fail_msg "output has: $1"; }
expect_unchanged() { [ "$(snapshot "$C/dir")" = "$BEFORE" ] || fail_msg "install directory changed"; }
expect_same() { cmp -s "$1" "$2" || fail_msg "$1 differs from $2"; }
expect_tail() { # expect_tail LINE...: the last lines of stdout, in order
  local want got
  want="$(printf '%s\n' "$@")"
  got="$(tail -n $# "$C/out")"
  [ "$got" = "$want" ] || fail_msg "stdout ends with [$(printf '%s' "$got" | tr '\n' '|')], want [$(printf '%s' "$want" | tr '\n' '|')]"
}
expect_short() {
  local n
  n=$(cat "$C/out" "$C/err" | wc -l | tr -d ' ')
  [ "$n" -le 6 ] || fail_msg "$n output lines, want at most 6"
}
expect_no_requests() { [ ! -s "$WWW/requests.log" ] || fail_msg "server got requests: $(tr '\n' ' ' <"$WWW/requests.log")"; }
expect_refused() { # expect_refused SENTENCE: exit 1, sentence, directory untouched
  expect_code 1
  expect_err "$1"
  expect_unchanged
}

# --- cases --------------------------------------------------------------------

case_installs_fresh() {
  tools default
  run_install -- --dir "$C/dir"
  expect_code 0
  expect_same "$C/dir/togo" "$W/bins/v1.0.0_${HOST_OS}_$HOST_ARCH"
  expect_tail "togo v1.0.0"
  expect_short
  [ "$(ls -A "$C/dir")" = togo ] || fail_msg "install directory holds $(ls -A "$C/dir" | tr '\n' ' ')"
  grep -q "/latest" "$WWW/requests.log" || fail_msg "latest release was not looked up"
}

case_upgrades_existing_atomically() {
  tools default
  prepare_dir
  ln "$C/dir/togo" "$C/old-link"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_code 0
  expect_same "$C/dir/togo" "$W/bins/v1.0.0_${HOST_OS}_$HOST_ARCH"
  # Replaced by rename: the old inode still holds the old binary.
  expect_same "$C/old-link" "$W/bins/v0.9.0_${HOST_OS}_$HOST_ARCH"
  [ "$(ls -A "$C/dir")" = togo ] || fail_msg "install directory holds $(ls -A "$C/dir" | tr '\n' ' ')"
}

case_refuses_tampered_archive() {
  tools default
  prepare_dir
  printf 'x' >>"$(host_archive)"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "does not match SHA256SUMS; nothing was installed."
}

case_refuses_tampered_sums() {
  tools default
  prepare_dir
  # The attacker swaps the archive and updates its SHA256SUMS line to match.
  local a sum
  a="$(host_archive)"
  printf 'x' >>"$a"
  sum="$("$OPENSSL" dgst -sha256 -r "$a" | cut -d ' ' -f 1)"
  awk -v n="$(basename "$a")" -v s="$sum" '$2 == n { $0 = s "  " n } { print }' \
    "$WWW/download/v1.0.0/SHA256SUMS" >"$C/sums" && cp "$C/sums" "$WWW/download/v1.0.0/SHA256SUMS"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "SHA256SUMS for togo v1.0.0 is not signed by the togo release key; nothing was installed."
}

case_refuses_other_key() {
  tools default
  prepare_dir
  sign other "$WWW/download/v1.0.0/SHA256SUMS" "togo v1.0.0 SHA256SUMS"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "is not signed by the togo release key"
}

case_refuses_old_version_comment() {
  tools default
  prepare_dir
  # A genuine, validly signed v0.9.0 SHA256SUMS served for v1.0.0.
  cp "$W/rel/v0.9.0/SHA256SUMS" "$W/rel/v0.9.0/SHA256SUMS.minisig" "$WWW/download/v1.0.0/"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused 'SHA256SUMS is signed for "togo v0.9.0 SHA256SUMS", not togo v1.0.0; refusing a mismatched or older release.'
}

# old_sums_relabelled: v0.9.0's genuine SHA256SUMS and signature served for
# v1.0.0, with the trusted comment edited to say v1.0.0.
old_sums_relabelled() {
  cp "$W/rel/v0.9.0/SHA256SUMS" "$WWW/download/v1.0.0/"
  sed 's/^trusted comment: togo v0.9.0 SHA256SUMS$/trusted comment: togo v1.0.0 SHA256SUMS/' \
    "$W/rel/v0.9.0/SHA256SUMS.minisig" >"$WWW/download/v1.0.0/SHA256SUMS.minisig"
}

case_refuses_rewritten_comment() {
  tools default
  prepare_dir
  old_sums_relabelled
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "SHA256SUMS for togo v1.0.0 is not signed by the togo release key; nothing was installed."
}

case_refuses_truncated_download() {
  tools default
  prepare_dir
  : >"$(host_archive).truncate"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "could not download togo_1.0.0_${HOST_OS}_$HOST_ARCH.tar.gz for togo v1.0.0"
}

case_refuses_missing_sig() {
  tools default
  prepare_dir
  mv "$WWW/download/v1.0.0/SHA256SUMS.minisig" "$C/"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "could not download SHA256SUMS.minisig for togo v1.0.0"
}

case_refuses_http_redirect() {
  tools default
  prepare_dir
  # A redirect that leaves https (here: to plain http) is never followed.
  mkdir -p "$WWW/download/plain"
  cp "$(host_archive)" "$WWW/download/plain/"
  printf '%s\n' "http://127.0.0.1:$PORT/$CASE_ID/download/plain/$(basename "$(host_archive)")" >"$(host_archive).redirect"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "could not download togo_1.0.0_"
  ! grep -q "/download/plain/" "$WWW/requests.log" 2>/dev/null || fail_msg "followed a redirect to http"
}

case_refuses_env_override_on_remote_url() {
  tools default
  wrap curl
  prepare_dir
  local url
  for url in "http://localhost:$PORT/$CASE_ID" "http://127.0.0.1:$PORT@localhost:$PORT/$CASE_ID" \
    "http://127.0.0.1.nip.io:$PORT/$CASE_ID" "https://github.com/gettogo/togo/releases" "file:///$WWW" ""; do
    ENV=(TOGO_INSTALL_BASE_URL="$url" TOGO_INSTALL_TEST_KEY="$TEST_KEY")
    run_install -- --version v1.0.0 --dir "$C/dir"
    expect_refused "only work with a base URL on 127.0.0.1; unset them and run this again."
  done
  local var
  for var in TOGO_INSTALL_TEST_KEY TOGO_INSTALL_TEST_REQUIREMENT TOGO_INSTALL_TEST_SYSTEM_DIR; do
    ENV=("$var=x")
    run_install -- --version v1.0.0 --dir "$C/dir"
    expect_refused "only work with a base URL on 127.0.0.1"
  done
  [ ! -e "$C/curl.log" ] || fail_msg "curl ran: $(tr '\n' ' ' <"$C/curl.log")"
  expect_no_requests
}

case_refuses_unknown_arch() {
  fakeable uname || return 0
  tools default
  prepare_dir
  local pair
  for pair in "Linux riscv64" "FreeBSD amd64" "Darwin i386"; do
    fake uname "case \$1 in -s) echo ${pair% *} ;; -m) echo ${pair#* } ;; esac"
    run_install -- --version v1.0.0 --dir "$C/dir"
    expect_refused "build it from source with: go install github.com/gettogo/togo/helper/cmd/togo@latest"
  done
  expect_no_requests
}

case_rosetta_picks_arm64() {
  fakeable uname || return 0
  tools default
  fake uname 'case $1 in -s) echo Darwin ;; -m) echo x86_64 ;; esac'
  fake sysctl '[ "$2" = sysctl.proc_translated ] && echo 1'
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_code 0
  expect_same "$C/dir/togo" "$W/bins/v1.0.0_darwin_arm64"
  # Without translation an x86_64 Mac gets amd64.
  fake sysctl '[ "$2" = sysctl.proc_translated ] && echo 0'
  run_install -- --version v1.0.0 --dir "$C/dir2"
  expect_code 0
  expect_same "$C/dir2/togo" "$W/bins/v1.0.0_darwin_amd64"
}

case_uses_shasum_without_sha256sum() {
  command -v shasum >/dev/null || { skip_case "no shasum on this host"; return; }
  fakeable sha256sum || return 0
  tools curl openssl
  wrap shasum
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_code 0
  grep -q -- "-a 256" "$C/shasum.log" 2>/dev/null || fail_msg "shasum -a 256 was not used"
}

# fake_wget gnu|busybox: wget on the sandbox PATH, curl absent.
fake_wget() {
  link_tool sha256sum || link_tool shasum
  tools openssl
  fake wget "exec '$PYTHON' -I '$here/install-test/fake_wget.py' $1 '$C/wget.log' \"\$@\""
}

case_uses_wget_without_curl() {
  fakeable wget || return 0
  fake_wget gnu
  run_install -- --dir "$C/dir"
  expect_code 0
  expect_same "$C/dir/togo" "$W/bins/v1.0.0_${HOST_OS}_$HOST_ARCH"
  grep -q -- "--https-only" "$C/wget.log" 2>/dev/null || fail_msg "wget ran without --https-only"
}

case_uses_busybox_wget() {
  fake_wget busybox
  run_install -- --dir "$C/dir"
  expect_code 0
  expect_same "$C/dir/togo" "$W/bins/v1.0.0_${HOST_OS}_$HOST_ARCH"
  expect_tail "togo v1.0.0"
}

case_refuses_http_redirect_with_wget() {
  local flavor
  for flavor in gnu busybox; do
    fake_wget "$flavor"
    prepare_dir
    mkdir -p "$WWW/download/plain"
    cp "$(host_archive)" "$WWW/download/plain/"
    printf '%s\n' "http://127.0.0.1:$PORT/$CASE_ID/download/plain/$(basename "$(host_archive)")" >"$(host_archive).redirect"
    run_install -- --version v1.0.0 --dir "$C/dir"
    expect_refused "the download of togo_1.0.0_${HOST_OS}_$HOST_ARCH.tar.gz was redirected to plain http; refusing it."
  done
}

case_openssl_path_without_minisign() {
  tools default
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_code 0
  expect_out "(minisign signature, checked with OpenSSL)"
}

case_minisign_path() {
  [ -n "$MINISIGN" ] || { skip_case "no minisign; set TOGO_INSTALL_TEST_MINISIGN"; return; }
  tools curl
  link_tool sha256sum || link_tool shasum
  link_tool minisign "$MINISIGN"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_code 0
  expect_out "(minisign signature)"
  prepare_dir
  sign other "$WWW/download/v1.0.0/SHA256SUMS" "togo v1.0.0 SHA256SUMS"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "is not signed by the togo release key"
  cp "$W/rel/v0.9.0/SHA256SUMS" "$W/rel/v0.9.0/SHA256SUMS.minisig" "$WWW/download/v1.0.0/"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused 'is signed for "togo v0.9.0 SHA256SUMS", not togo v1.0.0'
  old_sums_relabelled
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "is not signed by the togo release key"
}

case_codesign_path_on_darwin() {
  [ -n "$CODESIGN" ] || { skip_case "macOS only"; return; }
  tools curl codesign
  link_tool sha256sum || link_tool shasum
  link_libressl
  ENV+=(TOGO_INSTALL_TEST_REQUIREMENT='identifier "app.togo.helper"')
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_code 0
  expect_out "(Apple code signature)"
  expect_same "$C/dir/togo" "$W/bins/v1.0.0_${HOST_OS}_$HOST_ARCH"

  # The shipped requirement compiles, pins Developer ID, and rejects an
  # ad-hoc signature with the right identifier.
  local req
  req="$(sed -n "s/^TEAM_REQUIREMENT='\\(.*\\)'\$/\\1/p" "$INSTALL_SH")"
  case "$(csreq -r "=$req" -t 2>&1)" in
    # csreq prints the team id bare or quoted depending on its first character.
    *'1.2.840.113635.100.6.2.6'*'1.2.840.113635.100.6.1.13'*'subject.OU] = WULW83YF8M'* | \
    *'1.2.840.113635.100.6.2.6'*'1.2.840.113635.100.6.1.13'*'subject.OU] = "WULW83YF8M"'*) ;;
    *) fail_msg "TEAM_REQUIREMENT does not compile to a Developer ID requirement" ;;
  esac
  prepare_dir
  ENV=(TOGO_INSTALL_BASE_URL="$BASE" TOGO_INSTALL_TEST_KEY="$TEST_KEY")
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "the downloaded togo is not signed by the togo developer; nothing was installed."
  ENV+=(TOGO_INSTALL_TEST_REQUIREMENT='identifier "app.togo.helper"')

  run_install TOGO_INSTALL_TEST_REQUIREMENT='identifier "app.togo.other"' -- --version v1.0.0 --dir "$C/dir"
  expect_refused "the downloaded togo is not signed by the togo developer; nothing was installed."

  # A properly signed older binary passed off as v1.0.0, with sums to match.
  local a old
  a="$(host_archive)"
  old="$W/rel/v0.9.0/togo_0.9.0_${HOST_OS}_$HOST_ARCH.tar.gz"
  cp "$old" "$a"
  printf '%s  %s\n' "$("$OPENSSL" dgst -sha256 -r "$a" | cut -d ' ' -f 1)" "$(basename "$a")" >"$WWW/download/v1.0.0/SHA256SUMS"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "the downloaded togo is not version v1.0.0; refusing a mismatched or older release."
}

case_refuses_without_any_verifier() {
  tools curl
  link_tool sha256sum || link_tool shasum
  link_libressl
  prepare_dir
  run_install -- --version v1.0.0 --dir "$C/dir" # and no codesign on PATH
  expect_refused "Install minisign and run this again."
  expect_no_requests
}

case_installs_to_togo_bin_by_default() {
  [ "$(id -u)" != 0 ] || { skip_case "root can write a read-only directory"; return; }
  tools default
  # ~/.togo/bin is where the app looks first; a read-only system directory
  # is left alone.
  mkdir -p "$C/usr-local-bin"
  chmod 555 "$C/usr-local-bin"
  ENV+=(TOGO_INSTALL_TEST_SYSTEM_DIR="$C/usr-local-bin")
  run_install -- --version v1.0.0
  expect_code 0
  expect_same "$C/home/.togo/bin/togo" "$W/bins/v1.0.0_${HOST_OS}_$HOST_ARCH"
  expect_err "$C/home/.togo/bin is not on your PATH"
  expect_tail "togo v1.0.0"
  [ -z "$(ls -A "$C/usr-local-bin")" ] || fail_msg "wrote to the read-only directory"
  # A writable system directory gets a link, for the terminal's PATH.
  mkdir -p "$C/writable"
  ENV=(TOGO_INSTALL_BASE_URL="$BASE" TOGO_INSTALL_TEST_KEY="$TEST_KEY" TOGO_INSTALL_TEST_SYSTEM_DIR="$C/writable")
  run_install -- --version v1.0.0
  expect_code 0
  [ -h "$C/writable/togo" ] && [ "$(readlink "$C/writable/togo")" = "$C/home/.togo/bin/togo" ] ||
    fail_msg "no link from the writable system directory"
  # A binary an older install left in ~/.local/bin becomes a link too, so
  # a service or PATH that runs it gets the new version.
  mkdir -p "$C/home/.local/bin"
  cp "$W/bins/v1.0.0_${HOST_OS}_$HOST_ARCH" "$C/home/.local/bin/togo"
  run_install -- --version v1.0.0
  expect_code 0
  [ -h "$C/home/.local/bin/togo" ] || fail_msg "the old ~/.local/bin binary was not linked"
}

case_restarts_daemon_when_service_installed() {
  tools default
  run_install TOGO_FAKE_SERVICE=installed TOGO_FAKE_LOG="$C/togo.log" -- --version v1.0.0 --dir "$C/dir"
  expect_code 0
  grep -qx "daemon restart --when-idle" "$C/togo.log" || fail_msg "daemon was not restarted"
  expect_tail "togo v1.0.0"
  expect_no_out "Next: togo service install"
  expect_short
  : >"$C/togo.log"
  run_install TOGO_FAKE_SERVICE=installed TOGO_FAKE_LOG="$C/togo.log" -- --version v1.0.0 --dir "$C/dir" --no-restart
  expect_code 0
  ! grep -q "daemon restart" "$C/togo.log" || fail_msg "--no-restart restarted the daemon"
  : >"$C/togo.log"
  run_install TOGO_FAKE_LOG="$C/togo.log" -- --version v1.0.0 --dir "$C/dir"
  ! grep -q "daemon restart" "$C/togo.log" || fail_msg "restarted without an installed service"
}

case_installs_service_by_default() {
  tools default
  run_install TOGO_FAKE_LOG="$C/togo.log" -- --version v1.0.0 --dir "$C/dir"
  expect_code 0
  grep -qx "service install" "$C/togo.log" || fail_msg "service was not installed"
  expect_no_out "Next:"
  : >"$C/togo.log"
  run_install TOGO_FAKE_LOG="$C/togo.log" -- --version v1.0.0 --dir "$C/dir" --no-service
  expect_code 0
  ! grep -q "service install" "$C/togo.log" || fail_msg "--no-service installed the service"
  expect_tail "togo v1.0.0" "Next: $C/dir/togo service install"
  : >"$C/togo.log"
  run_install TOGO_FAKE_LOG="$C/togo.log" -- --version v1.0.0 --dir "$C/dir" --service
  expect_code 0
  grep -qx "service install" "$C/togo.log" || fail_msg "--service was not accepted"
}

case_runs_from_a_pipe() {
  tools default
  # As in curl ... | sh: the script arrives on stdin, and a togo that reads
  # stdin must not swallow the rest of it.
  PIPED=1 run_install TOGO_FAKE_READ_STDIN=1 -- --version v1.0.0 --dir "$C/dir"
  expect_code 0
  expect_tail "togo v1.0.0"
}

case_refuses_symlinked_togo() {
  tools default
  mkdir -p "$C/dir" "$C/cellar"
  cp "$W/bins/v0.9.0_${HOST_OS}_$HOST_ARCH" "$C/cellar/togo"
  ln -s "$C/cellar/togo" "$C/dir/togo"
  BEFORE="$(snapshot "$C/dir")"
  run_install -- --version v1.0.0 --dir "$C/dir"
  expect_refused "is a link, likely from a package manager; upgrade togo there, or pass --dir."
  expect_same "$C/cellar/togo" "$W/bins/v0.9.0_${HOST_OS}_$HOST_ARCH"
}

case_runs_nothing_when_truncated() {
  tools default
  prepare_dir
  # A connection that drops mid-script: every cut must run nothing at all.
  local full n cut
  full="$INSTALL_SH"
  n=$(wc -l <"$full" | tr -d ' ')
  for cut in 20 60 100 150 200 250 300 $((n - 2)) $((n - 1)); do
    head -n "$cut" "$full" >"$C/cut.sh"
    INSTALL_SH="$C/cut.sh" PIPED=1 run_install -- --version v1.0.0 --dir "$C/dir"
    [ ! -s "$C/out" ] || fail_msg "cut at line $cut printed: $(head -n 1 "$C/out")"
    expect_unchanged
  done
  INSTALL_SH="$full"
  expect_no_requests
}

case_rejects_bad_usage() {
  tools default
  local args
  for args in "--frobnicate" "--version" "--version 1.0.0" "--version v1.0" "--dir" "extra"; do
    # shellcheck disable=SC2086
    run_install -- $args
    expect_code 2
  done
  expect_no_requests
}
