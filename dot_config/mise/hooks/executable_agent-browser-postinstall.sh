#!/bin/bash
# mise postinstall hook for npm:agent-browser (dot_config/mise/config.toml).
# mise runs it after every install/upgrade of the CLI, with that version already
# on PATH.
set -e

# Fetch the Chrome for Testing build this CLI version expects.
agent-browser install

# Each version fetches a fresh ~350 MB Chrome and never removes the old ones
# (eight had piled up, 2.8 GB), and `doctor --fix` leaves them too. Keep only
# the build doctor reports in use; if that line is unreadable, prune nothing.
inuse="$(agent-browser doctor 2>/dev/null | grep -o '/[^ ]*/browsers/chrome-[0-9.]*' | head -1)"
if [ -n "$inuse" ] && [ -d "$inuse" ]; then
  for d in "$(dirname "$inuse")"/chrome-*; do
    [ "$d" = "$inuse" ] || { echo "==> Removing unused $(basename "$d")"; rm -rf "$d"; }
  done
fi
