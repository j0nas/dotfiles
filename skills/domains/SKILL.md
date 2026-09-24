---
name: domains
description: Check whether domain names are registered or available, and look up TLD registration/renewal pricing. Triggers on "is X.com available", "domain availability", "check domain", "is this domain taken", "find a free domain", "how much does a .io cost", "domain price".
user-invocable: true
allowed-tools: Bash, Read
---

# Domain availability

Check whether domains are free and what they cost **at the registrar you'd buy
them from**: Cloudflare Registrar first (at cost), Namecheap for the TLDs
Cloudflare doesn't sell, and the registries' own RDAP/whois as a last resort.

## How to use

Run the bundled script:

```bash
~/.claude/skills/domains/scripts/domain-check.sh example.com foo.io
```

- **Literal domains** (contain a dot) are checked as-is.
- **Bare labels** (no dot) are expanded across a TLD set:
  ```bash
  domain-check.sh -t com,net,io,ai,dev myname     # myname.com, myname.io, …
  domain-check.sh myname                            # default set: com,net,org,io,ai,dev,app,co
  ```
- Prices are always shown (`-p`/`--price` is still accepted and does nothing).
- `--no-browser` skips the Namecheap step (no browser tabs opened).
- `--json` emits `[{"domain","status","available","registration","renewal",
  "retail","currency","source","note"}]` for scripting.

Output: one row per domain with status, price and **source**:

```
commando.cards   ✓ available $30.20/yr                  cloudflare
commado.gg       ✓ available $68.98/yr                  namecheap
foo.de           ✓ available $5.98, retail $6.98/yr     namecheap
bar.gg           ✓ available $51.80/yr                  whois  (Porkbun list price)
```

Status is `AVAILABLE`, `TAKEN` or `UNKNOWN` (rate-limited or unparseable:
re-run or check manually). Exit codes: `0` all resolved, `1` at least one
`UNKNOWN`, `2` usage error.

## How it works

1. **Cloudflare Registrar** `POST /accounts/{id}/registrar/domain-check` (API
   beta since April 2026): authoritative, real-time, at-cost registration and
   renewal prices, premium-aware, 20 names per request. Needs a token with
   **Account › Registrar: Domains › Read** (read is enough for checks; it can't
   buy anything). Credentials come from `~/.claude/secrets/cloudflare.json`
   (`account_id`, `registrar_token`; age-encrypted in chezmoi as
   `dot_claude/private_secrets/encrypted_private_cloudflare.json.age`) or the env
   vars `CLOUDFLARE_REGISTRAR_TOKEN` + `CLOUDFLARE_ACCOUNT_ID`.
   - `registrable: true` → **AVAILABLE** with price; `domain_unavailable` →
     **TAKEN**.
   - `domain_premium` is **not** proof a name is free: Cloudflare answers it for
     registered premium names too (edh.dev, registered since 2024) and quotes no
     price. Those go to step 2 (Namecheap knows both), then step 3.
   - `extension_not_supported` (Cloudflare doesn't sell the TLD, e.g. `.gg`,
     `.de`) or `extension_not_supported_via_api` (dashboard only) → step 2.
   - Cloudflare's own CLI can do the same, but `cf` 0.13 (`npx cf`) is a preview
     with a quirk: `cf registrar registrations check x --body
     '{"domains":["a.com"]}'` needs a throwaway positional *and* `--body`, and the
     domain list in the positional is ignored. The script uses the REST call.
2. **Namecheap**, only for what Cloudflare doesn't sell. Its site is behind a
   Cloudflare bot check that curl and headless Chrome both fail ("Just a
   moment…"), and its API needs an account with API access and an allowlisted
   IP. So the script drives a **real browser over CDP**: Brave with
   `--remote-debugging-port=9222` on this Mac (override the port with
   `NAMECHEAP_CDP_PORT`). It opens a tab on the search results page, reads the
   `article.available|unavailable` row for the name and closes the tab. Regular
   rows read `$5.98/yr Retail $6.98/yr` (first year, and the undiscounted price,
   which isn't necessarily the renewal); premium rows `$227.50 Renews at
   $19.50/yr`. Skipped when nothing listens on the port, or with
   `--no-browser`.
3. **RDAP/whois** for whatever is still unanswered (and for everything when
   there are no Cloudflare credentials, e.g. on another machine):
   - RDAP straight to the TLD's registry, found in IANA's bootstrap
     (`data.iana.org/rdap/dns.json`, cached ~1 day in `$TMPDIR`): `200` →
     **TAKEN**, `404` → **AVAILABLE**, anything else → whois.
   - whois for TLDs without RDAP (e.g. `.gg`, `.de`): "no match / not found /
     status: free" → **AVAILABLE**; registration fields → **TAKEN**; rate-limit
     markers → **UNKNOWN**. RDAP-only TLDs (`.app`, `.dev`, `.cards`…) have no
     whois server, and macOS whois then prints IANA's record for the TLD itself;
     that's **UNKNOWN**, never TAKEN (reading its `nserver:` lines as a
     registration once marked every unregistered `.app`/`.cards` name as taken).
   - The price shown is **Porkbun's public list price for the TLD**
     (`api.porkbun.com/api/json/v3/pricing/get`, cached ~1 day): an estimate at a
     third registrar, never premium-aware. Watch the renewal trap (`.xyz`: $1.00,
     renews $12.98).

## Caveats — read before quoting "available"

- An **RDAP/whois "available"** means "not currently registered": premium,
  reserved and registry-locked names can be unregistered yet cost a lot or be
  ineligible. Cloudflare and Namecheap answers already account for that.
- Prices differ by registrar: Cloudflare charges the registry's price (e.g.
  `.cards` $30.20 where Namecheap asks $40.98); Namecheap's first-year promos
  often renew higher.
- A domain on the aftermarket (parked, "make offer") shows as **TAKEN**.
- whois can rate-limit on bursts; a flurry of `UNKNOWN` usually means back off
  and retry, not that the domains are free.
- A domain can be registered but have no website/DNS — registration status (this
  skill) is the right signal, not a `dig`/ping check.
- Requires `curl`, `jq` and `whois`; the Namecheap step also `agent-browser`
  (all installed by the dotfiles bootstrap).
