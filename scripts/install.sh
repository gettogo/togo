#!/bin/sh
# Install the togo helper from a GitHub release, verifying every download.
#
#   curl -fsSL https://github.com/gettogo/togo/releases/latest/download/install.sh | sh
#   sh install.sh [--version vX.Y.Z] [--dir DIR] [--no-restart] [--no-service]
#
# Unless --no-service is given, the helper's login service is installed too
# (togo service install), so needs-you alerts arrive while the app is closed.
# Through a pipe: curl -fsSL https://togoapp.dev/install.sh | sh -s -- --no-service
#
# The default directory is ~/.togo/bin, where the Togo app looks first; a
# link from /usr/local/bin (when writable) puts it on the terminal PATH.
#
# SHA256SUMS must carry a minisign signature by the release key below whose
# trusted comment is exactly "togo <version> SHA256SUMS", and the archive must
# match its line in SHA256SUMS. The signature is checked with minisign, or with
# OpenSSL 3 when minisign is missing. On macOS without either, the binary must
# instead pass codesign against the togo Developer ID. Anything else is refused,
# and nothing is written to the install directory before verification passes.
#
# Exit status: 0 installed, 1 refused, 2 usage. When piped into sh, the rest of
# this script is on stdin, so nothing it runs may read stdin.
set -eu

RELEASE_KEY='RWSrP5h2npeZJPo2xMRBOcppXrPGo6sJLR5Q4kMAM7KPq+kpCCDGFVfH'
# Developer ID Application only (intermediate 6.2.6, leaf 6.1.13): Apple
# Development and Distribution certificates of the same team do not match.
TEAM_REQUIREMENT='identifier "app.togo.helper" and anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] exists and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = "WULW83YF8M"'
RELEASES='https://github.com/gettogo/togo/releases'
FROM_SOURCE='go install github.com/gettogo/togo/helper/cmd/togo@latest'
# Largest download accepted, in bytes.
MAX_DOWNLOAD=209715200
# DER prefix of an Ed25519 SubjectPublicKeyInfo, base64.
ED25519_SPKI_PREFIX='MCowBQYDK2VwAyEA'

say() { printf '%s\n' "$*"; }
warn() { printf 'togo install: %s\n' "$*" >&2; }
refuse() { printf 'togo install: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'usage: sh install.sh [--version vX.Y.Z] [--dir DIR] [--no-restart] [--no-service]\n' >&2
  exit 2
}

valid_version() {
  case $1 in v[0-9]*) ;; *) return 1 ;; esac
  case $1 in *[!0-9A-Za-z.-]*) return 1 ;; esac
  printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
}

# loopback_url accepts http(s)://127.0.0.1[:port][/path] and nothing else: no
# user info, query, fragment or other host can slip through.
loopback_url() {
  case $1 in *[!A-Za-z0-9:/._-]*) return 1 ;; esac
  case $1 in
    http://*) rest=${1#http://} ;;
    https://*) rest=${1#https://} ;;
    *) return 1 ;;
  esac
  hostport=${rest%%/*}
  case $hostport in
    127.0.0.1) return 0 ;;
    127.0.0.1:*)
      port=${hostport#127.0.0.1:}
      case $port in '' | *[!0-9]*) return 1 ;; esac
      return 0
      ;;
  esac
  return 1
}

# Everything runs from main, called on the last line, so a download cut short
# defines functions at most and runs nothing.
main() {
  version=""
  dir=""
  restart=yes
  with_service=yes
  while [ $# -gt 0 ]; do
    case $1 in
      --version) [ $# -ge 2 ] || usage; version=$2; shift 2 ;;
      --version=*) version=${1#--version=}; shift ;;
      --dir) [ $# -ge 2 ] && [ -n "$2" ] || usage; dir=$2; shift 2 ;;
      --dir=?*) dir=${1#--dir=}; shift ;;
      --no-restart) restart=no; shift ;;
      --no-service) with_service=no; shift ;;
      --service) shift ;; # the default now; accepted for older instructions
      -h | --help)
        say 'usage: sh install.sh [--version vX.Y.Z] [--dir DIR] [--no-restart] [--no-service]'
        exit 0
        ;;
      *) usage ;;
    esac
  done
  if [ -n "$version" ] && ! valid_version "$version"; then
    printf 'togo install: --version must look like v1.2.3\n' >&2
    usage
  fi

  # Overrides for local tests only. Outside a 127.0.0.1 base URL their mere
  # presence is refused, so a planted variable cannot swap the key or the source.
  base=$RELEASES
  key=$RELEASE_KEY
  requirement=$TEAM_REQUIREMENT
  system_dir=/usr/local/bin
  test_mode=no
  if [ -n "${TOGO_INSTALL_BASE_URL+x}" ] || [ -n "${TOGO_INSTALL_TEST_KEY+x}" ] ||
    [ -n "${TOGO_INSTALL_TEST_REQUIREMENT+x}" ] || [ -n "${TOGO_INSTALL_TEST_SYSTEM_DIR+x}" ]; then
    if ! loopback_url "${TOGO_INSTALL_BASE_URL:-}"; then
      refuse "TOGO_INSTALL_BASE_URL and TOGO_INSTALL_TEST_* only work with a base URL on 127.0.0.1; unset them and run this again."
    fi
    test_mode=yes
    base=${TOGO_INSTALL_BASE_URL%/}
    key=${TOGO_INSTALL_TEST_KEY:-$key}
    requirement=${TOGO_INSTALL_TEST_REQUIREMENT:-$requirement}
    system_dir=${TOGO_INSTALL_TEST_SYSTEM_DIR:-$system_dir}
  fi
  case $key in '' | *[!A-Za-z0-9+/=]*) refuse "the release key is malformed; download install.sh again." ;; esac

  # Platform.
  os_name=$(uname -s 2>/dev/null || true)
  arch_name=$(uname -m 2>/dev/null || true)
  case $os_name in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) os="" ;;
  esac
  case $arch_name in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) arch="" ;;
  esac
  if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
    [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
    arch=arm64 # an x86_64 shell under Rosetta on Apple silicon
  fi
  if [ -z "$os" ] || [ -z "$arch" ]; then
    refuse "there is no prebuilt togo for $os_name $arch_name; build it from source with: $FROM_SOURCE"
  fi

  # Tools.
  if command -v curl >/dev/null 2>&1; then
    downloader=curl
  elif command -v wget >/dev/null 2>&1; then
    downloader=wget
    # BusyBox wget (Alpine) takes none of GNU wget's long options.
    if wget --help 2>&1 | grep -q BusyBox; then
      wget_https=""
    else
      wget_https=--https-only
    fi
  else
    refuse "Install curl or wget and run this again."
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    hasher=sha256sum
  elif command -v shasum >/dev/null 2>&1; then
    hasher=shasum
  else
    refuse "Install sha256sum or shasum and run this again."
  fi
  # OpenSSL 3 or later that can do BLAKE2b and Ed25519 (FIPS builds cannot).
  openssl_ok() {
    command -v openssl >/dev/null 2>&1 || return 1
    case $(openssl version 2>/dev/null) in 'OpenSSL '[3-9].*) ;; *) return 1 ;; esac
    openssl dgst -blake2b512 </dev/null >/dev/null 2>&1 &&
      openssl genpkey -algorithm ed25519 >/dev/null 2>&1
  }
  if command -v minisign >/dev/null 2>&1; then
    verifier=minisign
  elif openssl_ok; then
    verifier=openssl
  elif [ "$os" = darwin ] && command -v codesign >/dev/null 2>&1; then
    verifier=codesign
  else
    refuse "Install minisign and run this again."
  fi

  # The test server is plain http on 127.0.0.1; everything else is https only,
  # and a redirect may never leave https.
  proto='=https'
  [ "$test_mode" = no ] || proto='=http,https'

  # fetch URL FILE: 0 done, 1 failed, 3 redirected to plain http.
  fetch() {
    if [ "$downloader" = curl ]; then
      curl -fsSL --proto "$proto" --proto-redir '=https' --tlsv1.2 --max-filesize "$MAX_DOWNLOAD" -o "$2" "$1" 2>/dev/null
      return
    fi
    # wget cannot be told to stay on https when following redirects
    # (--https-only only governs recursive mode), so its server responses
    # are read back and any redirect to http is refused afterwards. The
    # signature check stays the integrity guard either way.
    wget_ok=yes
    wget $wget_https -S -O "$2" "$1" 2>"$2.log" || wget_ok=no
    if redirected_to_http "$2.log"; then return 3; fi
    [ "$wget_ok" = yes ] || return 1
    [ "$(wc -c <"$2" | tr -d ' ')" -le "$MAX_DOWNLOAD" ] || return 1
  }
  redirected_to_http() { grep -iq '^ *location: *http:' "$1"; }

  latest_version() {
    if [ "$downloader" = curl ]; then
      location=$(curl -sS --proto "$proto" --proto-redir '=https' --tlsv1.2 -o /dev/null -w '%{redirect_url}' "$base/latest" 2>/dev/null || true)
    else
      # Only the last path segment of the redirect is used, and validated.
      location=$(wget $wget_https -S --spider "$base/latest" 2>&1 |
        sed -n 's/^ *[Ll]ocation: *//p' | tr -d '\r' | head -n 1 || true)
    fi
    location=${location%/}
    printf '%s\n' "${location##*/}"
  }

  if [ -z "$version" ]; then
    version=$(latest_version)
    valid_version "$version" || refuse "could not find the latest togo release at $base; pass --version vX.Y.Z or check your connection."
  fi
  plain=${version#v}
  archive="togo_${plain}_${os}_${arch}.tar.gz"

  tmp=$(mktemp -d 2>/dev/null || mktemp -d -t togo-install)
  [ -n "$tmp" ] && [ -d "$tmp" ] || refuse "could not create a temporary directory."
  staged=""
  cleanup() {
    [ -z "$staged" ] || rm -f "$staged"
    rm -rf "$tmp"
  }
  trap cleanup EXIT
  trap 'exit 1' HUP INT TERM

  say "Downloading togo $version for $os/$arch"
  for name in "$archive" SHA256SUMS SHA256SUMS.minisig; do
    status=0
    fetch "$base/download/$version/$name" "$tmp/$name" || status=$?
    case $status in
      0) ;;
      3) refuse "the download of $name was redirected to plain http; refusing it." ;;
      *)
        if [ "$downloader" = wget ]; then
          refuse "wget could not download $name for togo $version; check the version and your connection, or install curl and run this again."
        fi
        refuse "could not download $name for togo $version; check the version and your connection, then run this again."
        ;;
    esac
  done

  # Signature over SHA256SUMS.
  not_signed() {
    refuse "SHA256SUMS for togo $version is not signed by the togo release key; nothing was installed."
  }
  check_comment() {
    if [ "$1" != "togo $version SHA256SUMS" ]; then
      refuse "SHA256SUMS is signed for \"$1\", not togo $version; refusing a mismatched or older release."
    fi
  }

  verify_minisign() {
    out=$(cd "$tmp" && minisign -Vm SHA256SUMS -P "$key" 2>/dev/null) || not_signed
    comment=$(printf '%s\n' "$out" | sed -n 's/^Trusted comment: //p')
    check_comment "$comment"
  }

  hex_of() { od -An -v -tx1 "$1" | tr -d ' \n'; }
  b64_to() {
    case $1 in '' | *[!A-Za-z0-9+/=]*) return 1 ;; esac
    printf '%s' "$1" | openssl base64 -d -A >"$2" 2>/dev/null
  }
  size_is() { [ "$(wc -c <"$1" | tr -d ' ')" = "$2" ]; }
  ed25519_verify() { # key.der message signature
    openssl pkeyutl -verify -pubin -keyform DER -inkey "$1" -rawin -in "$2" -sigfile "$3" >/dev/null 2>&1
  }

  # verify_openssl checks a minisign "ED" signature by hand: Ed25519 over the
  # BLAKE2b-512 of SHA256SUMS, then Ed25519 over signature || trusted comment.
  verify_openssl() {
    v=$tmp/verify
    mkdir "$v"
    tr -d '\r' <"$tmp/SHA256SUMS.minisig" >"$v/sig.txt"
    [ "$(awk 'END { print NR }' "$v/sig.txt")" = 4 ] || not_signed
    l1=$(sed -n 1p "$v/sig.txt")
    l2=$(sed -n 2p "$v/sig.txt")
    l3=$(sed -n 3p "$v/sig.txt")
    l4=$(sed -n 4p "$v/sig.txt")
    case $l1 in 'untrusted comment: '*) ;; *) not_signed ;; esac
    case $l3 in 'trusted comment: '*) ;; *) not_signed ;; esac
    comment=${l3#trusted comment: }

    b64_to "$key" "$v/key.bin" && size_is "$v/key.bin" 42 || not_signed
    b64_to "$l2" "$v/sig.bin" && size_is "$v/sig.bin" 74 || not_signed
    b64_to "$l4" "$v/global.bin" && size_is "$v/global.bin" 64 || not_signed
    key_hex=$(hex_of "$v/key.bin")
    sig_hex=$(hex_of "$v/sig.bin")
    [ "$(printf '%s' "$key_hex" | cut -c1-4)" = 4564 ] || not_signed # "Ed"
    [ "$(printf '%s' "$sig_hex" | cut -c1-4)" = 4544 ] || not_signed # "ED", prehashed
    [ "$(printf '%s' "$key_hex" | cut -c5-20)" = "$(printf '%s' "$sig_hex" | cut -c5-20)" ] || not_signed

    b64_to "$ED25519_SPKI_PREFIX" "$v/key.der" || not_signed
    tail -c 32 "$v/key.bin" >>"$v/key.der"
    tail -c 64 "$v/sig.bin" >"$v/sig64.bin"
    openssl dgst -blake2b512 -binary "$tmp/SHA256SUMS" >"$v/digest.bin" 2>/dev/null || not_signed
    ed25519_verify "$v/key.der" "$v/digest.bin" "$v/sig64.bin" || not_signed
    { cat "$v/sig64.bin" && printf '%s' "$comment"; } >"$v/global.msg"
    ed25519_verify "$v/key.der" "$v/global.msg" "$v/global.bin" || not_signed
    check_comment "$comment"
  }

  case $verifier in
    minisign) verify_minisign; how="minisign signature" ;;
    openssl) verify_openssl; how="minisign signature, checked with OpenSSL" ;;
    codesign) how="Apple code signature" ;; # checked on the binary below
  esac

  # Archive checksum.
  expected=$(awk -v n="$archive" 'NF == 2 && $2 == n { print $1; c++ } END { if (c != 1) exit 1 }' "$tmp/SHA256SUMS") ||
    refuse "SHA256SUMS has no single entry for $archive; nothing was installed."
  case $expected in *[!0-9a-f]*) refuse "SHA256SUMS is malformed; nothing was installed." ;; esac
  [ "${#expected}" = 64 ] || refuse "SHA256SUMS is malformed; nothing was installed."
  if [ "$hasher" = sha256sum ]; then
    actual=$(sha256sum "$tmp/$archive" | cut -d ' ' -f 1)
  else
    actual=$(shasum -a 256 "$tmp/$archive" | cut -d ' ' -f 1)
  fi
  [ "$actual" = "$expected" ] || refuse "$archive does not match SHA256SUMS; nothing was installed."

  mkdir "$tmp/x"
  tar -xzf "$tmp/$archive" -C "$tmp/x" togo 2>/dev/null || refuse "could not unpack $archive; nothing was installed."
  [ -f "$tmp/x/togo" ] && [ ! -h "$tmp/x/togo" ] || refuse "$archive holds no togo binary; nothing was installed."
  chmod 755 "$tmp/x/togo"

  if [ "$verifier" = codesign ]; then
    # No signature check is possible for SHA256SUMS here, so the binary itself
    # must be the togo Developer ID build, and of the version asked for.
    codesign --verify --strict -R "=$requirement" "$tmp/x/togo" >/dev/null 2>&1 ||
      refuse "the downloaded togo is not signed by the togo developer; nothing was installed."
    [ "$("$tmp/x/togo" version </dev/null 2>/dev/null || true)" = "togo $version" ] ||
      refuse "the downloaded togo is not version $version; refusing a mismatched or older release."
  fi
  say "Verified $archive ($how)"

  # Install directory: chosen only now, so a refusal above never touches it.
  # ~/.togo/bin is where the Togo app looks first (an SSH command's PATH
  # does not read shell profiles), so that is the default; a link from a
  # directory on PATH is added below for the terminal.
  default_dir=no
  if [ -z "$dir" ]; then
    [ -n "${HOME:-}" ] || refuse "HOME is not set; pass --dir DIR."
    dir=$HOME/.togo/bin
    default_dir=yes
  fi
  case $dir in /*) ;; *) dir=$(pwd)/$dir ;; esac
  mkdir -p "$dir" 2>/dev/null || refuse "could not create $dir; pass --dir with a directory you own."
  [ -w "$dir" ] || refuse "$dir is not writable; pass --dir with a directory you own (this script never uses sudo)."
  [ ! -d "$dir/togo" ] || refuse "$dir/togo is a directory; move it away or pass --dir."
  # A link here usually belongs to a package manager (Homebrew): leave it be.
  [ ! -h "$dir/togo" ] || refuse "$dir/togo is a link, likely from a package manager; upgrade togo there, or pass --dir."

  staged=$dir/.togo.install.$$
  cp "$tmp/x/togo" "$staged" && chmod 755 "$staged" || refuse "could not write to $dir; nothing was installed."
  mv -f "$staged" "$dir/togo" || refuse "could not replace $dir/togo; the old binary is unchanged."
  staged=""
  say "Installed $dir/togo"
  on_path=no
  case ":${PATH:-}:" in *":$dir:"*) on_path=yes ;; esac
  if [ "$default_dir" = yes ]; then
    # An earlier install put a plain binary in /usr/local/bin or
    # ~/.local/bin, which a service or PATH may still run: point it here.
    # With none, link from /usr/local/bin when it is writable.
    linked=no
    for d in "$system_dir" "$HOME/.local/bin"; do
      if [ -f "$d/togo" ] && [ ! -h "$d/togo" ] && [ -w "$d" ]; then
        ln -sf "$dir/togo" "$d/togo.link.$$" && mv -f "$d/togo.link.$$" "$d/togo" &&
          say "Linked $d/togo to $dir/togo" && linked=$d
      elif [ -h "$d/togo" ] && [ "$(readlink "$d/togo")" = "$dir/togo" ]; then
        linked=$d
      fi
    done
    if [ "$linked" = no ] && [ -d "$system_dir" ] && [ -w "$system_dir" ] && [ ! -e "$system_dir/togo" ]; then
      ln -s "$dir/togo" "$system_dir/togo" && say "Linked $system_dir/togo to $dir/togo" && linked=$system_dir
    fi
    if [ "$linked" != no ]; then
      case ":${PATH:-}:" in *":$linked:"*) on_path=yes ;; esac
    fi
  fi
  [ "$on_path" = yes ] || warn "$dir is not on your PATH; add it in your shell profile (export PATH=\"$dir:\$PATH\")."

  installed_version=$("$dir/togo" version </dev/null 2>/dev/null || true)
  service=$("$dir/togo" service status </dev/null 2>/dev/null || true)
  next=no
  case $service in
    Installed*)
      if [ "$restart" = no ]; then
        say "The running helper keeps the old version until: togo daemon restart --when-idle"
      elif "$dir/togo" daemon restart --when-idle </dev/null >/dev/null 2>&1; then
        say "The helper restarts on the new version once no request is pending"
      else
        warn "could not restart the helper; run: togo daemon restart --when-idle"
      fi
      ;;
    *)
      if [ "$with_service" = no ]; then
        next=yes
      elif ! "$dir/togo" service install </dev/null; then
        warn "could not install the login service; run: togo service install"
      fi
      ;;
  esac
  say "${installed_version:-togo $version}"
  [ "$next" = no ] || say "Next: $dir/togo service install"
}

main "$@"
