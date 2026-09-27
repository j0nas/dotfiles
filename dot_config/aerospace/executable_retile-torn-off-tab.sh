#!/bin/bash
# Tile a browser window that AeroSpace floated because it appeared mid tab-drag.
#
# Run by the on-window-detected callback in dot_aerospace.toml (with
# AEROSPACE_WINDOW_ID set) for Brave windows that were detected as floating.
#
# AeroSpace decides tile-vs-float once, when a window appears, and floats any
# window whose fullscreen button is disabled. Chromium keeps that button
# disabled on a torn-off tab's window until the drag settles, so the new window
# looks like a dialog at detection time. AeroSpace exempts Chrome from that
# check for exactly this race (isDialogHeuristic in AxUiElementWindowType.swift)
# but not Brave, so torn-off Brave tabs stayed floating for good.
#
# This re-asks AeroSpace's own verdict for a couple of seconds and tiles the
# window once it reads "window, not dialog". Real dialogs never flip, so they
# stay floating. `debug-windows` is not stable API: if its output format
# changes, the grep never matches and the window just stays floating, which is
# the behavior this replaces.
#
# Same minimal exec-and-forget PATH as new-wezterm-workspace.sh.

export PATH="/opt/homebrew/bin:/usr/bin:/bin:$PATH"
aerospace_bin="/opt/homebrew/bin/aerospace"
window_id="${AEROSPACE_WINDOW_ID:?}"

for _ in $(seq 20); do
  sleep 0.1
  # Fails once the window is gone (e.g. the tab was dropped back in).
  dump="$("$aerospace_bin" debug-windows --window-id "$window_id" 2>/dev/null)" || exit 0
  if grep -q '"Aero.AxUiElementWindowType" : "window"' <<<"$dump" &&
    grep -q '"Aero.AxUiElementWindowType_isDialogHeuristic" : false' <<<"$dump"; then
    exec "$aerospace_bin" layout --window-id "$window_id" tiling
  fi
done
