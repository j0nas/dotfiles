#!/bin/bash
# Tile a window that AeroSpace floated because it looked like a dialog when it
# appeared, once it no longer does.
#
# Run by the on-window-detected callback in dot_aerospace.toml (with
# AEROSPACE_WINDOW_ID set) for windows detected as floating, and by
# on-focus-changed when a floating window takes focus later.
#
# AeroSpace decides tile-vs-float once, when a window appears, and floats any
# window whose fullscreen button is disabled or whose subrole isn't standard
# (isDialogHeuristic in AxUiElementWindowType.swift). Windows can look like that
# for a moment: Chromium keeps the button disabled on a torn-off tab's window
# until the drag settles, and Fusion's main window floats from its launch while
# reading as a normal window once it's up. AeroSpace exempts Chrome from the
# check for the tab race, but no other app, so those windows stayed floating for
# good.
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
# on-focus-changed runs this on every focus change, so leave unless the window
# is floating. Focus on an empty workspace sets no window id.
window_id="${AEROSPACE_WINDOW_ID:-}"
[ -n "$window_id" ] || exit 0
"$aerospace_bin" list-windows --monitor all --format '%{window-id} %{window-layout}' |
  grep -qx "$window_id floating" || exit 0

for _ in $(seq 20); do
  sleep 0.1
  # Fails once the window is gone (e.g. the tab was dropped back in).
  dump="$("$aerospace_bin" debug-windows --window-id "$window_id" 2>/dev/null)" || exit 0
  if grep -q '"Aero.AxUiElementWindowType" : "window"' <<<"$dump" &&
    grep -q '"Aero.AxUiElementWindowType_isDialogHeuristic" : false' <<<"$dump"; then
    exec "$aerospace_bin" layout --window-id "$window_id" tiling
  fi
done
