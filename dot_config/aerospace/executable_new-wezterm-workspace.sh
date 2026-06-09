#!/bin/bash
# Open a new WezTerm window in the next empty workspace on the focused monitor.
#
# Bound to cmd-alt-enter in dot_aerospace.toml. AeroSpace assigns a new window to
# whichever workspace is focused when the window appears, so the trick is to
# switch to an empty workspace *first*, then spawn — the window lands there.
#
# AeroSpace's exec-and-forget gives GUI-launched processes a minimal PATH (no
# Homebrew, no shims), so binaries are resolved explicitly — same reason as the
# swiftbar label plugin.

export PATH="/opt/homebrew/bin:/usr/bin:/bin:$PATH"
aerospace_bin="/opt/homebrew/bin/aerospace"
wezterm_bin="/opt/homebrew/bin/wezterm"

# Lowest-numbered empty workspace on the monitor we're looking at. When the
# monitor is full this is empty and we just spawn in the current workspace.
empty="$("$aerospace_bin" list-workspaces --monitor focused --empty 2>/dev/null | head -n1)"
[ -n "$empty" ] && "$aerospace_bin" workspace "$empty"

# Prefer a new window in the already-running GUI (shares the mux, one process).
# If no GUI is up — or the mux socket isn't reachable from this minimal env —
# fall back to launching a fresh instance, which always yields a window.
"$wezterm_bin" cli spawn --new-window >/dev/null 2>&1 || open -na WezTerm
