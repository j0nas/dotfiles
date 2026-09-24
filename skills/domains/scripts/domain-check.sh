#!/usr/bin/env bash
# domain-check.sh — is a domain free, and what does it cost at a registrar you'd use?
#
# Sources, in order:
#   1. Cloudflare Registrar's domain-check API (authoritative, real-time, at-cost
#      price). Needs a Registrar: Domains Read token + account id, from
#      ~/.claude/secrets/cloudflare.json ({"account_id","registrar_token"}) or the
#      env vars CLOUDFLARE_REGISTRAR_TOKEN + CLOUDFLARE_ACCOUNT_ID.
#   2. Namecheap, for TLDs Cloudflare doesn't sell (e.g. .gg). Namecheap's site
#      sits behind a bot check that curl and headless Chrome fail, so this drives
#      a real browser over CDP (agent-browser --cdp, default port 9222, override
#      with NAMECHEAP_CDP_PORT); skipped when nothing listens there.
#   3. RDAP straight to the TLD's registry (IANA bootstrap, cached ~1 day), then
#      whois for TLDs without RDAP. Availability only; the price shown is
#      Porkbun's public list price for the TLD (an estimate, never premium-aware).
#      This is what every domain gets when there are no Cloudflare credentials.
#
# Usage:
#   domain-check.sh example.com foo.io            # check literal domains
#   domain-check.sh -t com,net,io,ai myname        # expand a bare label across TLDs
#   domain-check.sh --no-browser foo.gg            # skip Namecheap (use RDAP/whois)
#   domain-check.sh --json example.com             # machine-readable output
#   (-p/--price is accepted for compatibility; prices are always shown.)
#
# Status values: AVAILABLE | TAKEN | UNKNOWN
# Exit codes:    0 = every query resolved (AVAILABLE/TAKEN)
#                1 = at least one UNKNOWN
#                2 = usage error
#
# NOTE: "AVAILABLE" from RDAP/whois means "not currently registered": premium,
# reserved and registry-locked names can be unregistered yet not purchasable at
# list price. Cloudflare and Namecheap answers already account for that.
#
# RDAP-only TLDs (.app, .dev, .cards…) have no whois server: macOS whois then
# prints IANA's record for the TLD itself, whose nserver:/domain: lines must not
# be read as a registration (that once marked every free .app name as taken).

set -euo pipefail

DEFAULT_TLDS="com,net,org,io,ai,dev,app,co"
CF_API="https://api.cloudflare.com/client/v4"
CF_SECRETS="${HOME}/.claude/secrets/cloudflare.json"
CF_BATCH=20
RDAP_BASE="https://rdap.org/domain"
IANA_BOOTSTRAP="https://data.iana.org/rdap/dns.json"
PORKBUN_PRICING="https://api.porkbun.com/api/json/v3/pricing/get"
NC_PORT="${NAMECHEAP_CDP_PORT:-9222}"
TLDS="$DEFAULT_TLDS"
JSON=0
BROWSER=1

# Internal records are "|"-separated: a tab is IFS whitespace, so `read` would
# collapse empty fields between tabs and shift the rest.

usage() {
  # Print the leading comment block (the lines after the shebang up to the
  # first blank/non-comment line), stripped of the leading "# ".
  awk 'NR==1{next} /^#/{sub(/^# ?/,""); print; next} {exit}' "$0"
  exit "${1:-0}"
}

# ---- 1. Cloudflare Registrar ----

# cf_credentials -> sets CF_TOKEN and CF_ACCOUNT ("" when unavailable)
cf_credentials() {
  CF_TOKEN="${CLOUDFLARE_REGISTRAR_TOKEN:-}"
  CF_ACCOUNT="${CLOUDFLARE_ACCOUNT_ID:-}"
  if [ -z "$CF_TOKEN" ] && [ -r "$CF_SECRETS" ]; then
    CF_TOKEN=$(jq -r '.registrar_token // empty' "$CF_SECRETS" 2>/dev/null || true)
    CF_ACCOUNT=$(jq -r '.account_id // empty' "$CF_SECRETS" 2>/dev/null || true)
  fi
  if [ -z "$CF_TOKEN" ] || [ -z "$CF_ACCOUNT" ]; then CF_TOKEN=""; CF_ACCOUNT=""; fi
}

# cf_check DOMAIN... -> one line per domain the API answered for:
#   name|registrable|reason|registration|renewal|currency|tier
cf_check() {
  local body
  body=$(printf '%s\n' "$@" | jq -R . | jq -sc '{domains: .}')
  curl -sS --max-time 30 "$CF_API/accounts/$CF_ACCOUNT/registrar/domain-check" \
    -H "Authorization: Bearer $CF_TOKEN" -H 'Content-Type: application/json' \
    -d "$body" 2>/dev/null |
    jq -r 'select(.success) | .result.domains[] |
      [.name, .registrable, (.reason // ""), (.pricing.registration_cost // ""),
       (.pricing.renewal_cost // ""), (.pricing.currency // ""), (.tier // "")]
      | map(tostring) | join("|")' 2>/dev/null || true
}

# ---- 2. Namecheap, through a real browser ----

nc_ready() {
  [ "$BROWSER" -eq 1 ] && command -v agent-browser >/dev/null 2>&1 &&
    curl -s --max-time 2 "http://127.0.0.1:$NC_PORT/json/version" >/dev/null 2>&1
}

# nc_check DOMAIN -> "available|taken|forsale" | price | retail price | renewal | "premium"?
# Regular rows read "$5.98/yr Retail $6.98/yr"; premium ones "$227.50 Renews at $19.50/yr".
# A registered name resold on the aftermarket is also classed "available", with "Buy it
# now" / "Make offer" / lease-to-own terms ("Full price $5,999.00"): that's "forsale".
# (nothing when the page didn't answer). Opens its own tab and closes it.
nc_check() {
  local d="$1" out js
  export AGENT_BROWSER_SESSION="domains-nc-$$"
  # The results page renders one <article class="domain-<tld> available|unavailable">
  # per name; the searched one's text starts with the name itself.
  js="(() => { const a = [...document.querySelectorAll('article')].find(e =>
        (e.innerText || '').trim().split('\\n')[0].trim().toLowerCase() === '$d');
      return a ? JSON.stringify({c: a.className, t: a.innerText.replace(/\\s+/g, ' ')}) : ''; })()"
  agent-browser --cdp "$NC_PORT" tab new --label nc \
    "https://www.namecheap.com/domains/registration/results/?domain=$d" >/dev/null 2>&1 || return 0
  agent-browser wait --fn "!!($js)" >/dev/null 2>&1 || true
  out=$(agent-browser eval "$js" 2>/dev/null | jq -r '. // empty' 2>/dev/null || true)
  agent-browser tab close nc >/dev/null 2>&1 || true
  [ -n "$out" ] || return 0
  printf '%s' "$out" | jq -r '
    (.c | split(" ")) as $cls
    | (.t | test("Buy it now|Make offer|Lease to own"; "i")) as $resale
    | (if ($cls | index("unavailable")) then "taken"
       elif ($cls | index("available")) and $resale then "forsale"
       elif ($cls | index("available")) then "available" else "" end) as $s
    | select($s != "")
    | [$s,
       (if $s == "forsale"
        then ((.t | capture("Full price \\$(?<p>[0-9][0-9.,]*)") | .p)
              // ([.t | scan("\\$([0-9][0-9.,]*)") | .[0] | select(. != "0.00")] | first) // "")
        else ((.t | capture("\\$(?<p>[0-9][0-9.,]*)") | .p) // "") end),
       ((.t | capture("Retail \\$(?<p>[0-9.,]+)/yr") | .p) // ""),
       ((.t | capture("Renews at \\$(?<p>[0-9.,]+)/yr") | .p) // ""),
       (if (.t + " " + .c | test("premium"; "i")) then "premium" else "" end)]
    | join("|")' 2>/dev/null || true
}

nc_done() {
  AGENT_BROWSER_SESSION="domains-nc-$$" agent-browser close >/dev/null 2>&1 || true
}

# ---- 3. RDAP / whois ----

# rdap_status URL -> echoes HTTP status code (000 if unreachable)
rdap_status() {
  local code
  code=$(curl -sS -o /dev/null -L --max-time 15 \
    -w '%{http_code}' "$1" 2>/dev/null || true)
  [ -n "$code" ] || code="000"
  printf '%s' "$code"
}

# cached_json NAME URL JQ_TEST -> echoes the path of NAME in $TMPDIR, refreshed
# from URL when missing or older than ~1 day ("" if it was never fetched)
cached_json() {
  local f="${TMPDIR:-/tmp}/$1"
  if [ ! -f "$f" ] || [ -n "$(find "$f" -mtime +1 2>/dev/null)" ]; then
    if curl -sS --max-time 20 "$2" -o "$f.tmp" 2>/dev/null \
        && jq -e "$3" "$f.tmp" >/dev/null 2>&1; then
      mv "$f.tmp" "$f"
    fi
    rm -f "$f.tmp"
  fi
  if [ -f "$f" ]; then printf '%s' "$f"; fi
}

# rdap_server DOMAIN -> echoes the TLD registry's RDAP base URL ("" if it has none)
rdap_server() {
  [ -n "$BF" ] || return 0
  jq -r --arg t "${1##*.}" \
    'first(.services[] | select(any(.[0][]; . == $t)) | .[1][0]) // empty' "$BF"
}

# whois_classify DOMAIN -> echoes AVAILABLE | TAKEN | UNKNOWN
whois_classify() {
  local out
  out=$(whois "$1" 2>/dev/null || true)
  [ -n "$out" ] || { echo "UNKNOWN"; return; }

  if printf '%s' "$out" | grep -qiE 'rate.?limit|exceeded|too many|try again|quota|temporarily unavailable'; then
    echo "UNKNOWN"; return
  fi
  # IANA's record for the TLD with an empty "refer:" = the TLD has no whois
  # server, so this output says nothing about the domain itself.
  if printf '%s' "$out" | grep -qE '^refer:[[:space:]]*$'; then
    echo "UNKNOWN"; return
  fi
  # An exact "Domain Name: <domain>" line is a registry record for this very
  # domain — trust it over any "not found" noise. macOS whois concatenates every
  # referral hop, and a non-authoritative hop can answer "Object not found" for
  # a domain the registry hop shows as registered (seen with .me).
  if printf '%s' "$out" | grep -qiE "^[[:space:]]*domain name:[[:space:]]*$1[[:space:]]*\$"; then
    echo "TAKEN"; return
  fi
  if printf '%s' "$out" | grep -qiE 'no match|not found|no data found|no entries found|status:[[:space:]]*(free|available)|not registered|no object found|available for registration|domain not found|no such domain|object does not exist'; then
    echo "AVAILABLE"; return
  fi
  if printf '%s' "$out" | grep -qiE 'registry domain id|creation date|created:|registrar:|name server|nserver:|status:[[:space:]]*connect|registrant|^domain:'; then
    echo "TAKEN"; return
  fi
  echo "UNKNOWN"
}

# rdap_classify DOMAIN -> echoes "STATUS|source"
rdap_classify() {
  local domain="$1" server code
  server=$(rdap_server "$domain")
  if [ -n "$server" ]; then
    code=$(rdap_status "${server%/}/domain/$domain")
    case "$code" in
      200) echo "TAKEN|rdap"; return ;;
      404) echo "AVAILABLE|rdap"; return ;;  # the authoritative registry has no record
    esac
  elif [ -z "$BF" ]; then                    # no bootstrap: rdap.org, as before
    if [ "$(rdap_status "$RDAP_BASE/$domain")" = 200 ]; then echo "TAKEN|rdap"; return; fi
  fi
  echo "$(whois_classify "$domain")|whois"
}

# porkbun_price DOMAIN -> echoes "REGISTRATION RENEWAL" (USD) or "". Longest
# suffix wins, so co.uk beats uk.
porkbun_price() {
  local rest="${1#*.}"
  [ -n "$PF" ] || return 0
  while [ -n "$rest" ]; do
    if jq -e --arg k "$rest" '.pricing[$k]' "$PF" >/dev/null 2>&1; then
      jq -r --arg k "$rest" '.pricing[$k] | "\(.registration) \(.renewal)"' "$PF"
      return
    fi
    case "$rest" in
      *.*) rest="${rest#*.}" ;;
      *)   break ;;
    esac
  done
}

# ---- arg parsing ----
ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    -t|--tlds)    TLDS="$2"; shift 2 ;;
    -p|--price)   shift ;;
    --no-browser) BROWSER=0; shift ;;
    --json)       JSON=1; shift ;;
    -h|--help)    usage 0 ;;
    -*)           echo "unknown option: $1" >&2; usage 2 ;;
    *)            ARGS+=("$1"); shift ;;
  esac
done
[ "${#ARGS[@]}" -gt 0 ] || usage 2
command -v jq >/dev/null 2>&1 || { echo "domain-check.sh needs jq" >&2; exit 2; }

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
N=${#DOMAINS[@]}

# Per-domain results, index-aligned with DOMAINS (bash 3.2: no associative arrays).
STATUS=(); REG=(); REN=(); RET=(); CUR=(); SRC=(); NOTE=(); NEXT=()
for ((i = 0; i < N; i++)); do
  STATUS[i]=""; REG[i]=""; REN[i]=""; RET[i]=""; CUR[i]="USD"; SRC[i]=""; NOTE[i]=""; NEXT[i]="rdap"
done

index_of() {
  local j
  for ((j = 0; j < N; j++)); do
    if [ "${DOMAINS[j]}" = "$1" ]; then echo "$j"; return; fi
  done
  echo -1
}

# ---- 1. Cloudflare ----
cf_credentials
if [ -n "$CF_TOKEN" ]; then
  for ((b = 0; b < N; b += CF_BATCH)); do
    while IFS='|' read -r name ok reason reg ren cur tier; do
      i=$(index_of "$name"); [ "$i" -ge 0 ] || continue
      if [ "$ok" = true ]; then
        STATUS[i]=AVAILABLE; REG[i]="$reg"; REN[i]="$ren"; CUR[i]="${cur:-USD}"; SRC[i]=cloudflare
        if [ "$tier" = premium ]; then NOTE[i]="premium"; fi
        continue
      fi
      case "$reason" in
        domain_unavailable) STATUS[i]=TAKEN; SRC[i]=cloudflare ;;
        extension_disallows_registration)
          STATUS[i]=TAKEN; SRC[i]=cloudflare; NOTE[i]="registry closed to new registrations" ;;
        domain_premium)
          # Not proof it's free: Cloudflare answers this for registered premium names
          # too (edh.dev, registered since 2024), and quotes no price. Namecheap
          # knows both; RDAP settles it without a browser.
          NEXT[i]=namecheap; NOTE[i]="premium" ;;
        extension_not_supported) NEXT[i]=namecheap ;;
        extension_not_supported_via_api)
          NEXT[i]=namecheap; NOTE[i]="Cloudflare sells it in the dashboard only" ;;
      esac
    done < <(cf_check "${DOMAINS[@]:b:CF_BATCH}")
  done
fi

# ---- 2. Namecheap, for what Cloudflare doesn't sell (and premium names) ----
nc_used=0
for ((i = 0; i < N; i++)); do
  [ -z "${STATUS[i]}" ] && [ "${NEXT[i]}" = namecheap ] || continue
  if [ "$nc_used" -eq 0 ]; then
    nc_ready || break
    nc_used=1
  fi
  r=$(nc_check "${DOMAINS[i]}")
  [ -n "$r" ] || continue
  IFS='|' read -r s reg retail renews prem <<< "$r"
  SRC[i]=namecheap
  if [ "$s" = available ]; then
    # Namecheap shows a first-year price and its undiscounted "Retail" price,
    # not the renewal price.
    STATUS[i]=AVAILABLE; REG[i]="$reg"; RET[i]="$retail"; REN[i]="$renews"
    if [ -n "$prem" ]; then NOTE[i]="premium"; fi
  elif [ "$s" = forsale ]; then
    STATUS[i]=TAKEN; NOTE[i]="for sale${reg:+ at \$$reg} on the aftermarket"
  else
    STATUS[i]=TAKEN; NOTE[i]=""
  fi
done
if [ "$nc_used" -eq 1 ]; then nc_done; fi

# ---- 3. RDAP / whois for the rest ----
BF=""; PF=""; fetched=0
for ((i = 0; i < N; i++)); do
  [ -z "${STATUS[i]}" ] || continue
  if [ "$fetched" -eq 0 ]; then
    BF=$(cached_json iana-rdap-dns.json "$IANA_BOOTSTRAP" '.services | length > 0' || true)
    PF=$(cached_json porkbun-pricing.json "$PORKBUN_PRICING" '.status == "SUCCESS"' || true)
    fetched=1
  fi
  IFS='|' read -r s src <<< "$(rdap_classify "${DOMAINS[i]}")"
  STATUS[i]="$s"; SRC[i]="$src"
  if [ "$s" != AVAILABLE ]; then NOTE[i]=""; fi
  if [ "$s" = AVAILABLE ] && [ "${NOTE[i]}" = premium ]; then
    NOTE[i]="premium: price at the registrar"
  elif [ "$s" = AVAILABLE ]; then
    p=$(porkbun_price "${DOMAINS[i]}")
    if [ -n "$p" ]; then
      REG[i]="${p%% *}"; REN[i]="${p##* }"
      NOTE[i]="${NOTE[i]:+${NOTE[i]}; }Porkbun list price"
    fi
  fi
done

# ---- output ----
rc=0
if [ "$JSON" -eq 1 ]; then
  for ((i = 0; i < N; i++)); do
    if [ "${STATUS[i]}" = UNKNOWN ]; then rc=1; fi
    jq -nc --arg d "${DOMAINS[i]}" --arg s "${STATUS[i]}" --arg reg "${REG[i]}" \
      --arg ren "${REN[i]}" --arg ret "${RET[i]}" --arg cur "${CUR[i]}" --arg src "${SRC[i]}" \
      --arg note "${NOTE[i]}" \
      '{domain: $d, status: $s, available: ($s == "AVAILABLE"),
        registration: (if $reg == "" then null else $reg end),
        renewal: (if $ren == "" then null else $ren end),
        retail: (if $ret == "" then null else $ret end),
        currency: (if $reg == "" then null else $cur end),
        source: $src, note: (if $note == "" then null else $note end)}'
  done | jq -s .
else
  for ((i = 0; i < N; i++)); do
    case "${STATUS[i]}" in
      AVAILABLE) mark="✓ available" ;;
      TAKEN)     mark="✗ taken" ;;
      *)         mark="? unknown"; rc=1 ;;
    esac
    price=""
    if [ -n "${REG[i]}" ]; then
      sym='$'; [ "${CUR[i]}" = USD ] || sym="${CUR[i]} "
      price="${sym}${REG[i]}"
      if [ -n "${REN[i]}" ] && [ "${REN[i]}" != "${REG[i]}" ]; then
        price="$price, renews ${sym}${REN[i]}"
      elif [ -n "${RET[i]}" ] && [ "${RET[i]}" != "${REG[i]}" ]; then
        price="$price, retail ${sym}${RET[i]}"
      fi
      price="$price/yr"
    fi
    printf '%-28s %-12s %-28s %s%s\n' "${DOMAINS[i]}" "$mark" "$price" "${SRC[i]}" \
      "${NOTE[i]:+  (${NOTE[i]})}"
  done
fi
exit "$rc"
