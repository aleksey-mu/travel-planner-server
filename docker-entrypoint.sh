#!/bin/sh
set -e
DATA_DIR="${DATA_DIR:-/data}"
# Reject traversal segments before mkdir/chown.
case "$DATA_DIR" in
  *..*)
    echo "DATA_DIR must not contain '..' (got: $DATA_DIR)" >&2
    exit 1
    ;;
  /)
    echo "DATA_DIR must not be /" >&2
    exit 1
    ;;
  /data|/data/*)
    ;;
  *)
    echo "DATA_DIR must be /data or a path under /data (got: $DATA_DIR)" >&2
    exit 1
    ;;
esac
mkdir -p "$DATA_DIR"
# Resolve symlinks/relative segments; refuse anything outside /data.
resolved="$(realpath "$DATA_DIR")"
case "$resolved" in
  /data|/data/*)
    ;;
  *)
    echo "DATA_DIR resolves outside /data (got: $resolved)" >&2
    exit 1
    ;;
esac
chown -R appuser:appuser "$resolved"
exec su appuser -s /bin/sh -c 'exec /travelplan-server "$@"' -- "$@"
