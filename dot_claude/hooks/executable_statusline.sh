#!/bin/bash
# Claude Code statusLine. Shows model + reasoning effort + context window usage (via ccusage) plus
# the REAL 5h and weekly rate-limit pacing, read straight from the JSON Claude
# Code pipes to stdin (rate_limits.five_hour / .seven_day). Deliberately drops
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
ctx="$(printf '%s' "$input" | ccusage statusline 2>/dev/null \
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
printf '%b' "$out"
