#!/bin/bash
# Claude Code statusLine: directory | model/effort | context | 5-hour and weekly
# rate limits, all from the JSON Claude Code pipes to stdin, plus, on Fable
# sessions, that model's own weekly bucket ("F"). No cost figures: they mean
# nothing on a flat-rate subscription. A field stays empty until the first API
# response (rate_limits also on plans without them), and its segment is left out.
# Wired up via "statusLine" in Claude-settings.json; needs jq.

# One jq pass over the single-use stdin. Joined with the unit separator, because
# `read` would collapse empty fields between tabs.
IFS=$'\x1f' read -r dir model model_id effort ctx_pct ctx_tok f5 r5 w7 r7 < <(jq -r '
  [(.workspace.current_dir // .cwd), .model.display_name, .model.id, .effort.level,
   .context_window.used_percentage, .context_window.total_input_tokens,
   .rate_limits.five_hour.used_percentage, .rate_limits.five_hour.resets_at,
   .rate_limits.seven_day.used_percentage, .rate_limits.seven_day.resets_at]
  | map(. // "" | tostring) | join("\u001f")' 2>/dev/null)

# Color a usage %: <50 green, 50-79 yellow, >=80 red.
col() { if [ "$1" -ge 80 ]; then printf '\033[31m'; elif [ "$1" -ge 50 ]; then printf '\033[33m'; else printf '\033[32m'; fi; }
# Countdown to an epoch reset: "{D}d{H}h" a day or more out, "{H}h{M}m", "{M}m".
left() { local r=$(( ${1%.*} - $(date +%s) )); [ "$r" -lt 0 ] && r=0
  if   [ "$r" -ge 86400 ]; then printf '%dd%dh' $((r / 86400)) $(((r % 86400) / 3600))
  elif [ "$r" -ge 3600 ];  then printf '%dh%dm' $((r / 3600)) $(((r % 3600) / 60))
  else                          printf '%dm' $((r / 60)); fi; }
# Pace alert: a colored "!" when, at the average rate since the window opened, the
# quota runs out before it resets (projected % = used% / elapsed fraction, in
# basis points). Silent until 10% of the window has passed: too noisy before.
# args: used% resets_at window_seconds
pace() { [ -z "$2" ] && return; local used=$1 dur=$3
  local rem=$(( ${2%.*} - $(date +%s) )); [ "$rem" -lt 0 ] && rem=0
  local ef=$(( (dur - rem) * 10000 / dur )); [ "$ef" -lt 1000 ] && return
  local proj=$(( used * 10000 / ef ))
  if   [ "$proj" -gt 150 ]; then printf '\033[31m!\033[0m'
  elif [ "$proj" -gt 110 ]; then printf '\033[33m!\033[0m'; fi; }

out=""
add() { out="${out:+$out | }$1"; }
# A rate-limit segment, or nothing without a percentage.
# args: label used% resets_at window_seconds
limit() { [ -n "$2" ] || return 0
  local p; p=$(printf '%.0f' "$2")
  add "$1 $(col "$p")${p}%\033[0m$(pace "$p" "$3" "$4")${3:+/$(left "$3")}"; }

# The basename tells side-by-side sessions apart; it follows a `cd` mid-session.
[ "$dir" != "/" ] && dir="${dir%/}"; [ "$dir" != "/" ] && dir="${dir##*/}"
[ -n "$dir" ] && add "$dir"
[ -n "$model" ] && add "$model${effort:+/$effort}"
if [ -n "$ctx_pct" ]; then
  p=$(printf '%.0f' "$ctx_pct")
  add "ctx $(col "$p")${p}%\033[0m/$(( (${ctx_tok:-0} + 500) / 1000 ))K"
fi
limit 5h "$f5" "$r5" 18000
limit 7d "$w7" "$r7" 604800

# Fable's included-weekly bucket isn't in the stdin JSON, so ask the endpoint
# /usage reads, with the OAuth token (macOS keychain). Cached 5 min; a failed
# refresh keeps serving the old cache and an absent bucket drops the segment.
# curl -m 2 caps the stall, and $$ keeps concurrent renders off each other's file.
case "$model_id" in *fable*)
  cache="${XDG_CACHE_HOME:-$HOME/.cache}/claude-statusline-fable-usage.json"
  if [ -z "$(find "$cache" -mmin -5 2>/dev/null)" ]; then
    mkdir -p "${cache%/*}"
    tok="$(security find-generic-password -s "Claude Code-credentials" -w 2>/dev/null \
            | jq -r '.claudeAiOauth.accessToken // empty' 2>/dev/null)"
    if [ -n "$tok" ] && curl -sf -m 2 -H "Authorization: Bearer $tok" \
         -H "anthropic-beta: oauth-2025-04-20" \
         -o "$cache.tmp.$$" https://api.anthropic.com/api/oauth/usage 2>/dev/null; then
      mv "$cache.tmp.$$" "$cache"
    else rm -f "$cache.tmp.$$"; fi
  fi
  # resets_at is ISO 8601 ("...+00:00", fractional seconds); fromdateiso8601
  # takes only the "Z" form.
  IFS=$'\x1f' read -r fb fbr < <(jq -r '
      [.limits[]? | select(.kind == "weekly_scoped"
         and ((.scope.model.display_name // "") | test("fable"; "i")))][0]
      | [(.percent // ""),
         ((.resets_at // "") | sub("\\.[0-9]+"; "") | sub("\\+00:00$"; "Z")
          | (try fromdateiso8601 catch ""))] | map(tostring) | join("\u001f")' "$cache" 2>/dev/null)
  limit F "$fb" "$fbr" 604800
;; esac

printf '%b' "$out"
