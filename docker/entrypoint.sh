#!/bin/sh
# SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
# Starts HPK Console in a container (docs/docker.md).
#
#   HPK_OWNER=you@example.com   first start only: turn team mode on with this owner.
#                               The owner's token is written to /data/hpk/owner-token
#                               (mode 600), never to the log.
#   HPK_TLS_CERT, HPK_TLS_KEY   serve HTTPS with these files (recommended)
#   HPK_BEHIND_PROXY=1          plain HTTP for a reverse proxy that adds TLS
#   HPK_AUTH=header, HPK_HEADER, HPK_PROXY   sign-in by that proxy instead of tokens
set -eu

data="${HPK_HOME:-/data}"
addr="${HPK_ADDR:-0.0.0.0:7878}"

health() {
  port="${addr##*:}"
  if [ -n "${HPK_TLS_CERT:-}" ]; then
    wget -q -T 4 --no-check-certificate -O /dev/null "https://127.0.0.1:${port}/api/health"
  else
    wget -q -T 4 -O /dev/null "http://127.0.0.1:${port}/api/health"
  fi
}

case "${1:-serve}" in
  health)
    health
    exit $?
    ;;
  serve)
    ;;
  *)
    # Anything else is an hpk command: docker exec … hpk-entrypoint team list
    exec hpk "$@"
    ;;
esac

if [ ! -f "$data/hpk/team.yaml" ] && [ -n "${HPK_OWNER:-}" ]; then
  mkdir -p "$data/hpk"
  umask 077
  if [ "${HPK_AUTH:-token}" = header ]; then
    hpk team init --owner "$HPK_OWNER" --auth header --header "${HPK_HEADER:?set HPK_HEADER}" --proxy "${HPK_PROXY:?set HPK_PROXY}" > /dev/null
    echo "hpk: team mode on; $HPK_OWNER signs in through the proxy"
  else
    hpk team init --owner "$HPK_OWNER" | grep -o 'hpk_[0-9a-f]*' > "$data/hpk/owner-token"
    echo "hpk: team mode on; the owner's token is in $data/hpk/owner-token (read it once, then delete it)"
  fi
fi

if [ -n "${HPK_TLS_CERT:-}" ]; then
  exec hpk serve --addr "$addr" --tls-cert "$HPK_TLS_CERT" --tls-key "${HPK_TLS_KEY:?set HPK_TLS_KEY}"
elif [ "${HPK_BEHIND_PROXY:-}" = 1 ]; then
  exec hpk serve --addr "$addr" --insecure-http
else
  exec hpk serve --addr "$addr"
fi
