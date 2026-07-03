---
name: domains
description: Check whether domain names are registered or available, and look up TLD registration/renewal pricing. Triggers on "is X.com available", "domain availability", "check domain", "is this domain taken", "find a free domain", "how much does a .io cost", "domain price".
user-invocable: true
allowed-tools: Bash, Read
---

# Domain availability

Check whether one or more domains are currently registered, with no API key or
registrar account. Backed by **RDAP** (the modern WHOIS replacement, run by the
registries themselves) with a **whois fallback** for TLDs that lack RDAP.

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
- `-p` / `--price` adds **TLD registration & renewal pricing** next to each result:
  ```bash
  domain-check.sh -p myname.com bbc.co.uk          # reg $11.08 / renew $11.08 …
  domain-check.sh -p -t com,io,ai,xyz myname        # price the whole TLD spread
  ```
- `--json` emits `[{"domain","status","available"}]` (with `registration`/`renewal`
  fields added when `-p` is set) for scripting.

Output status is one of: `AVAILABLE` (not registered), `TAKEN` (registered),
`UNKNOWN` (rate-limited or unparseable — re-run or check manually).

Exit codes: `0` all resolved, `1` at least one `UNKNOWN`, `2` usage error.

## How it works

1. RDAP query via `rdap.org` (IANA bootstrap aggregator):
   - HTTP `200` → a registration record exists → **TAKEN**.
   - HTTP `404` / `429` / unreachable → fall through to whois.
2. whois disambiguates a 404 (which also happens for TLDs with no RDAP server,
   e.g. `.de`): registry "no match / not found / status: free" → **AVAILABLE**;
   registration fields (creation date, registrar, name servers…) → **TAKEN**;
   rate-limit markers → **UNKNOWN**.

## Pricing — how it works & prior art

`-p` pulls **standard TLD pricing from Porkbun's public, no-auth API**
(`api.porkbun.com/api/json/v3/pricing/get`): registration / renewal / transfer for
~900 TLDs, cached ~1 day in `$TMPDIR`. Longest-suffix matching means `co.uk` is
priced as `co.uk`, not `uk`. Watch the **renewal trap** — e.g. `.xyz` shows
`reg $1.00 / renew $12.98`.

There is **no single "price of a domain."** It varies by registrar, promo/coupon,
first-year vs renewal, and premium status. What's available where:

| Source | Auth | Covers | Notes |
| --- | --- | --- | --- |
| **Porkbun `/pricing/get`** (used here) | none | base price per TLD, ~900 TLDs | best free option; one registrar; no premium |
| Cloudflare Registrar API (Check) | account | per-domain incl. registry premium | at-cost; needs CF account |
| DNSimple `…/registrar/domains/:d/prices` | token | per-domain, `premium`+`premium_price` | cleanest premium-aware API |
| Namecheap `users.getPricing` + `domains.check` | API key + IP allowlist | base + `IsPremiumName`/`PremiumRegistrationPrice` | account requirements |
| GoDaddy availability (`checkType=FULL`) | account w/ **50+ domains** | per-domain incl. premium | API gated to 50+ domains since May 2024 — effectively unusable for casual use |
| TLDSpy / TLD-List / domaindetails | varies | cross-registrar comparison | aggregators, some paid APIs |

**Premium names** (registry-priced, e.g. short/dictionary words) are *not* reflected
by `-p` — Porkbun's bulk endpoint has no premium flag. Accurate per-name premium
pricing needs an authenticated registrar API (DNSimple or Cloudflare are the
cleanest). If you want that wired in, drop an API token and ask — the script is
structured to add a `price_source`.

## Caveats — read before quoting "available"

- **Available ≠ purchasable at base price.** Premium, reserved, and
  registry-locked names can be unregistered yet cost a lot or be ineligible.
  Always confirm final price/eligibility at a registrar before promising.
- whois can rate-limit on bursts; a flurry of `UNKNOWN` usually means back off
  and retry, not that the domains are free.
- A domain can be registered but have no website/DNS — registration status (this
  skill) is the right signal, not a `dig`/ping check.
- Requires `curl` and `whois` on PATH (both present on this machine).
