#!/usr/bin/env bash
# Shim: this hook's body lives in the Go binary now (`devbrain hook response`).
# The legacy bash implementation is hooks/legacy/capture-response.sh (golden generator until cutover).
HERE="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
BIN="${DEVBRAIN_BIN:-$HERE/../devbrain}"
[ -x "$BIN" ] || BIN="$(command -v devbrain)" || exit 0
exec "$BIN" hook response
