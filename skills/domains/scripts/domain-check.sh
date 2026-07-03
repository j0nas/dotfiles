#!/usr/bin/env bash
# domain-check.sh — check whether domains are registered, using RDAP (+ whois fallback).
#
# No API keys. Strategy:
#   1. Query RDAP via rdap.org (the IANA bootstrap aggregator).
#        HTTP 200  -> a registration record exists -> TAKEN.
#        HTTP 404  -> no record OR the TLD has no RDAP server -> confirm with whois.
#        other     -> fall back to whois.
#   2. whois disambiguates: registry "no match / not found / free" => AVAILABLE,
#      registration fields (creation date, registrar, name server…) => TAKEN,
#      rate-limited / unparseable => UNKNOWN.
#
# Usage:
#   domain-check.sh example.com foo.io            # check literal domains
#   domain-check.sh -t com,net,io,ai myname        # expand a bare label across TLDs
#   domain-check.sh -p example.com foo.io          # add Porkbun reg/renewal pricing
#   domain-check.sh --json example.com             # machine-readable output
#
# Pricing (-p/--price): standard TLD registration & renewal from Porkbun's public
# no-auth pricing API (cached ~1 day in $TMPDIR). This is the *base* TLD price at
# one registrar — it does NOT reflect premium/registry-priced names (those need an
# authenticated registrar API, e.g. DNSimple/Cloudflare) nor promos at other
# registrars. Treat it as a ballpark, not a quote.
#
# Status values: AVAILABLE | TAKEN | UNKNOWN
# Exit codes:    0 = every query resolved (AVAILABLE/TAKEN)
#                1 = at least one UNKNOWN
#                2 = usage error
#
# NOTE: "AVAILABLE" means "not currently registered" — it does NOT mean cheap or
# unrestricted. Premium, reserved, and registry-locked names can be unregistered
# yet not purchasable at base price. Confirm final price/eligibility at a registrar.

set -euo pipefail

DEFAULT_TLDS="com,net,org,io,ai,dev,app,co"
RDAP_BASE="https://rdap.org/domain"
PORKBUN_PRICING="https://api.porkbun.com/api/json/v3/pricing/get"
TLDS="$DEFAULT_TLDS"
JSON=0
PRICE=0

usage() {
  # Print the leading comment block (the lines after the shebang up to the
  # first blank/non-comment line), stripped of the leading "# ".
  awk 'NR==1{next} /^#/{sub(/^# ?/,""); print; next} {exit}' "$0"
  exit "${1:-0}"
}

# rdap_status DOMAIN -> echoes HTTP status code (000 if unreachable)
rdap_status() {
  local code
  code=$(curl -sS -o /dev/null -L --max-time 15 \
    -w '%{http_code}' "$RDAP_BASE/$1" 2>/dev/null || true)
  [ -n "$code" ] || code="000"
  printf '%s' "$code"
}

# whois_classify DOMAIN -> echoes AVAILABLE | TAKEN | UNKNOWN
whois_classify() {
  local out
  out=$(whois "$1" 2>/dev/null || true)
  [ -n "$out" ] || { echo "UNKNOWN"; return; }

  if printf '%s' "$out" | grep -qiE 'rate.?limit|exceeded|too many|try again|quota|temporarily unavailable'; then
    echo "UNKNOWN"; return
  fi
  if printf '%s' "$out" | grep -qiE 'no match|not found|no data found|no entries found|status:[[:space:]]*(free|available)|not registered|no object found|available for registration|domain not found|no such domain|object does not exist'; then
    echo "AVAILABLE"; return
  fi
  if printf '%s' "$out" | grep -qiE 'registry domain id|creation date|created:|registrar:|name server|nserver:|status:[[:space:]]*connect|registrant|^domain:'; then
    echo "TAKEN"; return
  fi
  echo "UNKNOWN"
}

# classify DOMAIN -> echoes AVAILABLE | TAKEN | UNKNOWN
classify() {
  local domain="$1" code
  code=$(rdap_status "$domain")
  case "$code" in
    200) echo "TAKEN" ;;
    *)   whois_classify "$domain" ;;   # 404/429/000/other -> whois confirms
  esac
}

# pricing_file -> echoes path to a cached Porkbun pricing JSON ("" if unavailable).
# Cached in $TMPDIR (or /tmp), refreshed when missing or older than ~1 day.
pricing_file() {
  local f="${TMPDIR:-/tmp}/porkbun-pricing.json"
  if [ ! -f "$f" ] || [ -n "$(find "$f" -mtime +1 2>/dev/null)" ]; then
    if curl -sS --max-time 20 "$PORKBUN_PRICING" -o "$f.tmp" 2>/dev/null \
        && jq -e '.status=="SUCCESS"' "$f.tmp" >/dev/null 2>&1; then
      mv "$f.tmp" "$f"
    fi
    rm -f "$f.tmp"
  fi
  [ -f "$f" ] && printf '%s' "$f"
}

# price_for DOMAIN PRICING_FILE -> echoes "REGISTRATION RENEWAL" (USD) or "" if
# the TLD isn't in the dataset. Uses longest-suffix match so co.uk beats uk.
price_for() {
  local rest="${1#*.}" pf="$2"
  while [ -n "$rest" ]; do
    if jq -e --arg k "$rest" '.pricing[$k]' "$pf" >/dev/null 2>&1; then
      jq -r --arg k "$rest" '.pricing[$k] | "\(.registration) \(.renewal)"' "$pf"
      return
    fi
    case "$rest" in
      *.*) rest="${rest#*.}" ;;
      *)   break ;;
    esac
  done
  printf ''
}

# ---- arg parsing ----
ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    -t|--tlds) TLDS="$2"; shift 2 ;;
    -p|--price) PRICE=1; shift ;;
    --json)    JSON=1; shift ;;
    -h|--help) usage 0 ;;
    -*)        echo "unknown option: $1" >&2; usage 2 ;;
    *)         ARGS+=("$1"); shift ;;
  esac
done
[ "${#ARGS[@]}" -gt 0 ] || usage 2

# ---- expand bare labels across TLDs; pass literal domains through ----
DOMAINS=()
for a in "${ARGS[@]}"; do
  a=$(printf '%s' "$a" | tr '[:upper:]' '[:lower:]')
  if [[ "$a" == *.* ]]; then
    DOMAINS+=("$a")
  else
    IFS=',' read -ra tlist <<< "$TLDS"
    for t in "${tlist[@]}"; do
      t="${t#.}"; [ -n "$t" ] && DOMAINS+=("$a.$t")
    done
  fi
done

# ---- pricing data (fetched once, if requested) ----
PF=""
if [ "$PRICE" -eq 1 ]; then
  PF=$(pricing_file)
  [ -n "$PF" ] || echo "warning: Porkbun pricing unavailable; prices omitted" >&2
fi

# ---- run ----
rc=0
if [ "$JSON" -eq 1 ]; then
  printf '['
  first=1
  for d in "${DOMAINS[@]}"; do
    s=$(classify "$d")
    [ "$s" = "UNKNOWN" ] && rc=1
    [ "$first" -eq 1 ] && first=0 || printf ','
    printf '{"domain":"%s","status":"%s","available":%s' \
      "$d" "$s" "$([ "$s" = AVAILABLE ] && echo true || echo false)"
    if [ "$PRICE" -eq 1 ]; then
      p=""; [ -n "$PF" ] && p=$(price_for "$d" "$PF")
      if [ -n "$p" ]; then
        printf ',"registration":"%s","renewal":"%s","currency":"USD","price_source":"porkbun"' \
          "${p%% *}" "${p##* }"
      else
        printf ',"registration":null,"renewal":null,"price_source":"porkbun"'
      fi
    fi
    printf '}'
  done
  printf ']\n'
else
  for d in "${DOMAINS[@]}"; do
    s=$(classify "$d")
    case "$s" in
      AVAILABLE) mark="✓ available" ;;
      TAKEN)     mark="✗ taken" ;;
      *)         mark="? unknown"; rc=1 ;;
    esac
    if [ "$PRICE" -eq 1 ]; then
      p=""; [ -n "$PF" ] && p=$(price_for "$d" "$PF")
      if [ -n "$p" ]; then
        priced=$(printf 'reg $%s / renew $%s' "${p%% *}" "${p##* }")
      else
        priced="price n/a"
      fi
      printf '%-32s %-13s %s\n' "$d" "$mark" "$priced"
    else
      printf '%-32s %s\n' "$d" "$mark"
    fi
  done
fi
exit "$rc"
