#!/bin/sh
set -eu

run="${RELAYDB_RUN:-api}"
export RELAYDB_SERVICE="${RELAYDB_SERVICE:-$run}"

case "$run" in
  api|capture|delivery|demo-commerce)
    exec "/$run"
    ;;
  *)
    echo "invalid RELAYDB_RUN: $run" >&2
    exit 64
    ;;
esac