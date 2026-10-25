#!/bin/sh
set -e
mkdir -p "$DATA_DIR"
chown -R appuser:appuser "$DATA_DIR"
exec su-exec appuser /travelplan-server "$@"
