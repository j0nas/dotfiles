#!/bin/bash
# Claude Code statusLine. Shows model + reasoning effort + context window usage (via ccusage) plus
# the REAL 5h and weekly rate-limit pacing, read straight from the JSON Claude
# Code pipes to stdin (rate_limits.five_hour / .seven_day), and — on Fable
# sessions only — the Fable 5 included-weekly bucket ("F5"), fetched from
# /api/oauth/usage since Claude Code doesn't pipe it to stdin. Deliberately drops
# ccusage's API-equivalent cost and burn-rate segments — those are meaningless
# on a flat-rate subscription (not your actual bill). rate_limits is only
# present for Pro/Max after the first API response, so the 5h/weekly segments
# are simply omitted until then. Needs ccusage + jq (both mise-managed); wired
# via "statusLine" in settings.json. stdin is single-use, so capture it once.
input="$(cat)"

# Model name comes straight from the stdin JSON. Context-window usage is the one
# thing only ccusage knows (it reads the transcript), so pull just that segment
# out of ccusage's " | " line and relabel it "ctx" (RS=" | " splits the line).
model="$(printf '%s' "$input" | jq -r '.model.display_name // empty' 2>/dev/null)"
# Reasoning effort, appended as "{model}/{level}" to match the "/"-joined
# sub-values in the other segments. Whatever string .effort.level holds is shown
# verbatim — no value list is hardcoded, so provider effort-level renames pass
# through untouched. The .effort object is absent for models without a
# reasoning-effort knob, so this stays empty and we skip it.
effort="$(printf '%s' "$input" | jq -r '.effort.level // empty' 2>/dev/null)"
[ -n "$effort" ] && model="$model/$effort"
# ccusage is a Node script (`#!/usr/bin/env node`). Claude Code spawns this
# statusline in a non-interactive shell that never ran `mise activate`, so bare
# `node` isn't on PATH and ccusage's shebang dies — silently (2>/dev/null), which
# is why the ctx segment vanished while the jq-only segments below kept working.
# Invoke ccusage through its mise shim (an absolute symlink to the mise binary,
# which sets node up itself); scoped to ccusage so the jq calls stay direct and
# don't pay mise's per-invocation overhead on every prompt render. Data-dir
# resolution mirrors mise's own: MISE_DATA_DIR > XDG_DATA_HOME > ~/.local/share.
mise_data="${MISE_DATA_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/mise}"
ctx="$(printf '%s' "$input" | "$mise_data/shims/ccusage" statusline 2>/dev/null \
  | awk -v RS=' \\| ' '/🧠/{sub(/^🧠[[:space:]]*/,""); print; exit}')"

# color a usage %: <50 green, 50-79 yellow, >=80 red. Same scale for context.
col() { if [ "$1" -ge 80 ]; then printf '\033[31m'; elif [ "$1" -ge 50 ]; then printf '\033[33m'; else printf '\033[32m'; fi; }
# countdown to an epoch reset, unpadded for terseness: "{D}d{H}h" a day+ out,
# "{H}h{M}m" within the day, "{M}m" under an hour.
left() { local r=$(( ${1%.*} - $(date +%s) )); [ "$r" -lt 0 ] && r=0
  if   [ "$r" -ge 86400 ]; then printf '%dd%dh' $((r / 86400)) $(((r % 86400) / 3600))
  elif [ "$r" -ge 3600 ];  then printf '%dh%dm' $((r / 3600)) $(((r % 3600) / 60))
  else                          printf '%dm' $((r / 60)); fi; }
# pace alert: a colored "!" when, at the average rate since this window opened,
# the quota would be exhausted before it resets. projected end-of-window % =
# used% / elapsed-fraction. Silent until >=10% elapsed (too noisy before that).
# args: used% resets_at window_seconds. Integer math in basis points (ef = frac*1e4).
pace() { [ -z "$2" ] && return; local used=$1 dur=$3
  local rem=$(( ${2%.*} - $(date +%s) )); [ "$rem" -lt 0 ] && rem=0
  local ef=$(( (dur - rem) * 10000 / dur )); [ "$ef" -lt 1000 ] && return
  local proj=$(( used * 10000 / ef ))
  if   [ "$proj" -gt 150 ]; then printf '\033[31m!\033[0m'
  elif [ "$proj" -gt 110 ]; then printf '\033[33m!\033[0m'; fi; }

IFS=$'\t' read -r f5 r5 w7 r7 < <(printf '%s' "$input" \
  | jq -r '[.rate_limits.five_hour.used_percentage, .rate_limits.five_hour.resets_at,
            .rate_limits.seven_day.used_percentage, .rate_limits.seven_day.resets_at]
           | map(. // "") | @tsv' 2>/dev/null)

# Fable 5 weekly bucket. Claude Code tracks the Fable included-weekly allowance
# internally (the weekly_scoped/seven_day_overage_included limit) but the
# statusline stdin JSON only ever carries five_hour/seven_day, so when the
# session model IS Fable we ask /api/oauth/usage ourselves — the same endpoint
# /usage reads — with the OAuth token (macOS keychain, else
# ~/.claude/.credentials.json). Cached 5 min via `find -mmin` (portable across
# BSD/GNU, unlike stat); a failed refresh keeps serving the stale cache and an
# absent bucket just drops the segment, so this never breaks the line. curl -m 2
# caps the once-per-5-min stall; $$ in the temp name keeps concurrent sessions'
# renders from clobbering each other mid-download.
model_id="$(printf '%s' "$input" | jq -r '.model.id // empty' 2>/dev/null)"
fb=""; fbr=""
case "$model_id" in *fable*)
  cache="${XDG_CACHE_HOME:-$HOME/.cache}/claude-statusline-fable-usage.json"
  if [ -z "$(find "$cache" -mmin -5 2>/dev/null)" ]; then
    mkdir -p "${cache%/*}"
    tok="$( { security find-generic-password -s "Claude Code-credentials" -w 2>/dev/null \
              || cat "$HOME/.claude/.credentials.json" 2>/dev/null; } \
            | jq -r '.claudeAiOauth.accessToken // empty' 2>/dev/null)"
    if [ -n "$tok" ] && curl -sf -m 2 -H "Authorization: Bearer $tok" \
         -H "anthropic-beta: oauth-2025-04-20" \
         -o "$cache.tmp.$$" https://api.anthropic.com/api/oauth/usage 2>/dev/null; then
      mv "$cache.tmp.$$" "$cache"
    else rm -f "$cache.tmp.$$"; fi
  fi
  # resets_at is ISO 8601 ("...+00:00", fractional seconds); normalize to the
  # "Z" form fromdateiso8601 accepts so left()/pace() get their epoch.
  IFS=$'\t' read -r fb fbr < <(jq -r '
      [.limits[]? | select(.kind == "weekly_scoped"
         and ((.scope.model.display_name // "") | test("fable"; "i")))][0]
      | [(.percent // ""),
         ((.resets_at // "") | sub("\\.[0-9]+"; "") | sub("\\+00:00$"; "Z")
          | (try fromdateiso8601 catch ""))] | @tsv' "$cache" 2>/dev/null)
;; esac

out="$model"
if [ -n "$ctx" ]; then
  # ccusage gives "150,462 (75%)"; reshape to "{pct}%/{tokens}K" to match 5h/7d.
  if [[ "$ctx" =~ ^(.+)[[:space:]]\(([0-9]+)%\)$ ]]; then
    tok="${BASH_REMATCH[1]//,/}"; cp="${BASH_REMATCH[2]}"
    ctx="$(col "$cp")${cp}%\033[0m/$(( (tok + 500) / 1000 ))K"
  fi
  out="${out:+$out | }ctx $ctx"
fi
[ -n "$f5" ] && { p=$(printf '%.0f' "$f5"); s="5h $(col "$p")${p}%\033[0m$(pace "$p" "$r5" 18000)"; [ -n "$r5" ] && s="$s/$(left "$r5")"; out="${out:+$out | }$s"; }
[ -n "$w7" ] && { p=$(printf '%.0f' "$w7"); s="7d $(col "$p")${p}%\033[0m$(pace "$p" "$r7" 604800)"; [ -n "$r7" ] && s="$s/$(left "$r7")"; out="${out:+$out | }$s"; }
[ -n "$fb" ] && { p=$(printf '%.0f' "$fb"); s="F5 $(col "$p")${p}%\033[0m$(pace "$p" "$fbr" 604800)"; [ -n "$fbr" ] && s="$s/$(left "$fbr")"; out="${out:+$out | }$s"; }
printf '%b' "$out"
