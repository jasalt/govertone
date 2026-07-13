#!/usr/bin/env bash
set -euo pipefail
if ! wpctl status >/dev/null 2>&1; then
  echo "No PipeWire sink; real-time smoke skipped"
  exit 0
fi
printf '(play :sine :a4 {:dur 1})\n' | "${LGS:-./lgs}" repl
