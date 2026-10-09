#!/bin/bash
# Open a new WezTerm window in the focused workspace.
#
# Bound to cmd-alt-enter in dot_aerospace.toml. AeroSpace assigns a new window to
# whichever workspace is focused when the window appears, so no workspace
# handling is needed here.
#
# AeroSpace's exec-and-forget gives GUI-launched processes a minimal PATH (no
# Homebrew, no shims), so binaries are resolved explicitly — same reason as the
# swiftbar label plugin.

export PATH="/opt/homebrew/bin:/usr/bin:/bin:$PATH"
wezterm_bin="/opt/homebrew/bin/wezterm"

# Prefer a new window in the already-running GUI (shares the mux, one process).
# If no GUI is up — or the mux socket isn't reachable from this minimal env —
# fall back to launching a fresh instance, which always yields a window.
"$wezterm_bin" cli spawn --new-window >/dev/null 2>&1 || open -na WezTerm
