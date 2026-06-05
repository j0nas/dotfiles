#!/bin/bash
# AeroSpace ad-hoc workspace labels — SwiftBar plugin that doubles as a CLI.
#
# AeroSpace has no native way to rename a workspace, and it draws nothing on
# screen, so this keeps a sidecar label store and renders it in the menu bar.
#
#   (no args) → SwiftBar render: "<workspace> · <label>" in the menu bar.
#   set       → prompt (osascript) and store a label for the focused workspace.
#   clear     → remove the focused workspace's label.
#
# Labels live in $XDG_STATE_HOME/aerospace/labels.json (workspace -> label) and
# persist across restarts. AeroSpace bindings (cmd-alt-shift-n / -u) call
# set/clear; the menu bar refreshes on workspace change (exec-on-workspace-change
# in dot_aerospace.toml) and after each set/clear via the refreshplugin URL.
#
# The filename encodes SwiftBar's refresh interval (aerospace.10s.sh) and the
# refresh URL name ("aerospace", the part before the first dot). If you rename
# it, update the exec paths in dot_aerospace.toml to match.

# GUI-launched processes (SwiftBar, AeroSpace) inherit a minimal PATH; make jq
# and aerospace resolvable explicitly. mise owns jq; Homebrew owns aerospace.
export PATH="/opt/homebrew/bin:$HOME/.local/share/mise/shims:/usr/bin:/bin:$PATH"

store="${XDG_STATE_HOME:-$HOME/.local/state}/aerospace/labels.json"
self="$0"
aerospace_bin="/opt/homebrew/bin/aerospace"

ensure_store() {
  mkdir -p "$(dirname "$store")"
  [ -f "$store" ] || printf '{}\n' > "$store"
}

focused() { "$aerospace_bin" list-workspaces --focused 2>/dev/null; }

label_for() { # <workspace>
  [ -f "$store" ] || return 0
  jq -r --arg k "$1" '.[$k] // ""' "$store" 2>/dev/null
}

refresh() { /usr/bin/open -g "swiftbar://refreshplugin?name=aerospace" 2>/dev/null || true; }

write_label() { # <workspace> <label>  (empty label deletes the entry)
  ensure_store
  local tmp
  tmp="$(mktemp)"
  if [ -n "$2" ]; then
    jq --arg k "$1" --arg v "$2" '.[$k] = $v' "$store" > "$tmp" && mv "$tmp" "$store"
  else
    jq --arg k "$1" 'del(.[$k])' "$store" > "$tmp" && mv "$tmp" "$store"
  fi
  rm -f "$tmp"
}

case "${1:-render}" in
  set)
    ws="$(focused)"
    [ -n "$ws" ] || exit 0
    cur="$(label_for "$ws")"
    # argv carries current label + workspace so no shell text reaches AppleScript.
    new="$(osascript - "$cur" "$ws" <<'OSA'
on run argv
  set cur to item 1 of argv
  set ws to item 2 of argv
  try
    set r to text returned of (display dialog "Label for workspace " & ws & " (empty to clear):" default answer cur with title "AeroSpace workspace label" buttons {"Cancel", "Set"} default button "Set")
    return r
  on error number -128
    return "__CANCEL__"
  end try
end run
OSA
)"
    [ "$new" = "__CANCEL__" ] && exit 0
    write_label "$ws" "$new"
    refresh
    ;;
  clear)
    ws="$(focused)"
    [ -n "$ws" ] || exit 0
    write_label "$ws" ""
    refresh
    ;;
  render | *)
    ws="$(focused)"
    [ -n "$ws" ] || { echo "—"; exit 0; }
    lbl="$(label_for "$ws")"
    # Menu bar title: workspace number, plus the label when one is set.
    if [ -n "$lbl" ]; then
      echo "$ws · $lbl | sfimage=tag length=40"
    else
      echo "$ws | sfimage=square.dashed length=40"
    fi
    echo "---"
    if [ -n "$lbl" ]; then
      echo "Workspace $ws — $lbl"
    else
      echo "Workspace $ws (no label)"
    fi
    echo "---"
    echo "Set label… | bash=$self param1=set terminal=false refresh=true"
    [ -n "$lbl" ] && echo "Clear label | bash=$self param1=clear terminal=false refresh=true"
    cnt="$(jq 'length' "$store" 2>/dev/null || echo 0)"
    if [ "${cnt:-0}" -gt 0 ]; then
      echo "---"
      echo "Labeled workspaces"
      jq -r 'to_entries[] | "\(.key)\t\(.value)"' "$store" 2>/dev/null |
        while IFS=$'\t' read -r k v; do
          mark=""
          [ "$k" = "$ws" ] && mark=" ✓"
          echo "$k · $v$mark | bash=$aerospace_bin param1=workspace param2=$k terminal=false refresh=true"
        done
    fi
    ;;
esac
