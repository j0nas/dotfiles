#!/bin/bash
# Claude Code statusLine. Shows model + context window usage (via ccusage) plus
# the REAL 5h and weekly rate-limit pacing, read straight from the JSON Claude
# Code pipes to stdin (rate_limits.five_hour / .seven_day). Deliberately drops
# ccusage's API-equivalent cost and burn-rate segments — those are meaningless
# on a flat-rate subscription (not your actual bill). rate_limits is only
# present for Pro/Max after the first API response, so the 5h/weekly segments
# are simply omitted until then. Needs ccusage + jq (both mise-managed); wired
# via "statusLine" in settings.json. stdin is single-use, so capture it once.
input="$(cat)"

# Keep only ccusage's model (🤖) and context (🧠) segments from its " | " line;
# drop the rest. RS=" | " splits ccusage's output into records.
base="$(printf '%s' "$input" | ccusage statusline 2>/dev/null \
  | awk -v RS=' \\| ' '/🤖|🧠/{a[n++]=$0} END{for(i=0;i<n;i++) printf "%s%s",(i?" | ":""),a[i]}')"

# color a usage %: <50 green, 50-79 yellow, >=80 red
col() { if [ "$1" -ge 80 ]; then printf '\033[31m'; elif [ "$1" -ge 50 ]; then printf '\033[33m'; else printf '\033[32m'; fi; }
# time until an epoch reset: "in HH:mm" when under a day away, else "Dd HHh".
left() { local r=$(( ${1%.*} - $(date +%s) )); [ "$r" -lt 0 ] && r=0
  if [ "$r" -ge 86400 ]; then printf '%dd %02dh' $((r / 86400)) $(((r % 86400) / 3600))
  else                        printf 'in %02d:%02d' $((r / 3600)) $(((r % 3600) / 60)); fi; }

IFS=$'\t' read -r f5 r5 w7 r7 < <(printf '%s' "$input" \
  | jq -r '[.rate_limits.five_hour.used_percentage, .rate_limits.five_hour.resets_at,
            .rate_limits.seven_day.used_percentage, .rate_limits.seven_day.resets_at]
           | map(. // "") | @tsv' 2>/dev/null)

out="$base"
[ -n "$f5" ] && { p=$(printf '%.0f' "$f5"); s="⏳ 5h $(col "$p")${p}%\033[0m"; [ -n "$r5" ] && s="$s ($(left "$r5"))"; out="$out | $s"; }
[ -n "$w7" ] && { p=$(printf '%.0f' "$w7"); s="🗓️ 7d $(col "$p")${p}%\033[0m"; [ -n "$r7" ] && s="$s ($(left "$r7"))"; out="$out | $s"; }
printf '%b' "$out"
