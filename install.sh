#!/bin/sh
# SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
#
# One-command installer for hpk (macOS and Linux):
#   curl -fsSL https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.sh | sh
#
# It downloads the release archive for this machine, checks its SHA-256 against
# checksums.txt (and stops if anything is off), then runs `hpk self-install`.
#
# Optional environment:
#   HPK_VERSION       install this version (e.g. 0.1.0) instead of the latest
#   HPK_RELEASE_BASE  download from here instead of GitHub (testing / mirrors)
#   HPK_DATA_DIR      where hpk keeps its files (default: the usual per-user place)
#   HPK_NO_START=1    install only; do not start hpk afterwards
set -eu

REPO="hellOoSaksit/HermesPiKa-WebUi"

say() { printf '%s\n' "$*"; }
die() { printf 'hpk install: %s\n' "$*" >&2; exit 1; }

tmp=""
cleanup() { if [ -n "$tmp" ]; then rm -rf "$tmp"; fi; }
trap cleanup EXIT
trap 'exit 1' INT TERM HUP

# fetch URL DEST -- the only place that touches the network.
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 2 -o "$2" "$1" || return 1
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$2" "$1" || return 1
  else
    die "need curl or wget to download hpk; install one and run this again"
  fi
}

download() {
  fetch "$1" "$2" || die "could not download $1 (check your internet connection, then run this again)"
}

case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) die "unsupported system '$(uname -s)': this script is for macOS and Linux (on Windows use install.ps1)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported CPU '$(uname -m)': hpk is built for amd64 and arm64" ;;
esac

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t hpk-install)" || die "could not create a temporary folder"

# Which version?
ver="${HPK_VERSION:-}"
if [ -z "$ver" ]; then
  download "https://api.github.com/repos/$REPO/releases/latest" "$tmp/latest.json"
  ver="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -n 1)"
  [ -n "$ver" ] || die "could not read the latest version from GitHub; set HPK_VERSION (for example HPK_VERSION=0.1.0)"
fi
ver="${ver#v}"
case "$ver" in
  *[!0-9A-Za-z.+_-]*|"") die "odd version '$ver'" ;;
esac

base="${HPK_RELEASE_BASE:-https://github.com/$REPO/releases/download/v$ver}"
base="${base%/}"
archive="hpk_${ver}_${os}_${arch}.tar.gz"

say "Installing hpk $ver ($os/$arch)..."
download "$base/$archive" "$tmp/$archive"
download "$base/checksums.txt" "$tmp/checksums.txt"

# Verify. Fail closed: no tool, no line, or a different hash all stop here.
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
else
  die "cannot verify the download: neither sha256sum nor shasum is installed"
fi
expected="$(awk -v n="$archive" '$2 == n || $2 == "*" n { print $1; exit }' "$tmp/checksums.txt")"
[ -n "$expected" ] || die "checksum for $archive is not listed in checksums.txt; refusing to install"
case "$expected" in
  *[!0-9a-fA-F]*) die "checksums.txt is malformed; refusing to install" ;;
esac
[ "${#expected}" -eq 64 ] || die "checksums.txt is malformed; refusing to install"
actual="$(printf '%s' "$actual" | tr 'A-F' 'a-f')"
expected="$(printf '%s' "$expected" | tr 'A-F' 'a-f')"
[ "$actual" = "$expected" ] || die "checksum mismatch for $archive (expected $expected, got $actual); refusing to install"
say "Checksum OK."

mkdir "$tmp/x"
tar -xzf "$tmp/$archive" -C "$tmp/x" || die "could not unpack $archive"
[ -f "$tmp/x/hpk" ] || die "the archive does not contain hpk"
chmod +x "$tmp/x/hpk"
if [ "$os" = darwin ]; then
  xattr -d com.apple.quarantine "$tmp/x/hpk" 2>/dev/null || true
fi

(cd "$tmp/x" && ./hpk self-install) || die "hpk self-install failed"

# Where self-install put it (same rule as hpk's own data folder).
if [ -n "${HPK_DATA_DIR:-}" ]; then
  data="$HPK_DATA_DIR"
elif [ "$os" = darwin ]; then
  data="${HOME:-/tmp}/Library/Application Support/hpk"
else
  data="${XDG_DATA_HOME:-${HOME:-/tmp}/.local/share}/hpk"
fi
target="$data/bin/hpk"

if [ "${HPK_NO_START:-}" = 1 ]; then
  say "Done. Start it any time with: $target"
  exit 0
fi
if [ -x "$target" ]; then
  say "Done. Starting hpk (open a new terminal later and just type: hpk)."
  "$target" &
else
  say "Done. Open a new terminal and type: hpk"
fi
