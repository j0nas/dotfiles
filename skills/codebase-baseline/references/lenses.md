# Wave 1 lenses

A lens is one scanner's topic. Pick the lenses that apply, size them to the budget, and write the choice to `run.json`. You choose the lenses because the user doesn't know the codebase yet. Every scanner reads `agents/scanner.md` for how to work and what to return. The lens text below is its topic.

## Catalog

**architecture**
- Components, entry points and deploy units, and how they talk to each other: sync, async, realtime.
- Request lifecycle, caching layers, background jobs and crons.
- Trace the 2–4 most important user-facing flows end to end, with file:line hops. These become sequence diagrams.
- Name the boundaries a change most often crosses.

**data** (xhigh)
- Schema and migrations: how many, whether reversible, drift between schema and migrations.
- Data access patterns: ORM vs raw SQL, N+1, transactions, locking, consistency for concurrent writes.
- Where time comes from (database or app clock), PII inventory, retention and deletion paths.
- Any in-flight data migration (dual writes, backfills) and its exit criteria.

**security** (xhigh)
- How identity is established and checked: sessions, tokens, middleware.
- Authorization: every public endpoint or action and how it's guarded. Produce the list with file:line.
- Input handling: injection, XSS (raw HTML), SSRF (user-controlled URLs), open redirects, path traversal, deserialisation.
- Secrets handling and keys in history.
- Rate limiting, CORS and CSP, and dependency advisories from the loop results.
- Look for recurring bug classes in git history: the same kind of check added by hand in several commits.

**quality**
- What the tests cover and what they don't: unit, integration or end-to-end; which layers.
- Type-safety holes (`any`, casts, unchecked inputs), dead code, duplication, unused dependencies.
- Error handling: swallowed errors, catch-all handlers, observability calls in code.
- Lint config strictness.
- Cross-reference the loop results; don't re-run them.

**frontend** (only with a UI)
- Rendering strategy, bundle size per route from the build output, client state, data fetching.
- Hot interactions, caching, accessibility basics, internationalisation.

**integrations**
- Every third-party service: SDK imports, HTTP clients, env var names (names only), webhooks.
- Every sibling repo or internal service referenced.
- For each: what it's used for, where, and what read access would let the report add.
- These rows seed the access register.

**delivery**
- From `snapshot.json` git data and CI config:
  - who commits;
  - how much is agent co-authored;
  - commit and PR cadence;
  - reverts and fix ratio;
  - recurring fix clusters (subjects and files that keep reappearing);
  - release process, branch strategy, what CI runs and what it skips.
- Set `history: true` on this lens in the workflow args. It's the one lens that needs history beyond the snapshot, so it gets read-only git access to the original repo.

**ops**
- Deploy config, environments and how they differ, IaC.
- Config and secret injection, monitoring and alerting configured in code, health checks.
- Graceful shutdown, scaling assumptions, backups.

## Optional lenses

Add one when the inventory shows the need: mobile app, ML or data pipeline, infrastructure-heavy repo, public API or SDK, embedded or firmware.

## Sizing

- **Split a lens by module** (package, service, top-level directory) when its share of the code exceeds about 50k lines. Each split gets `paths` (absolute paths inside the scan export) and its own unique key, e.g. `security-api`. Keys name the output files, so a duplicate would overwrite.
- **Merge thin lenses** for small repos. Under 20k lines, roughly 4–5 scanners in total: architecture+data, security, quality+frontend, integrations+ops+delivery.
- **Rough totals:**

| Repo size | Scanners |
|---|---|
| under 20k lines | 4–5 |
| 20–200k | 6–9 |
| 200k–1M | 10–16, with per-package splits |
| over 1M | per-package splits for the parts that matter; record the rest in `not_covered` |

- **Choosing what to cover in a huge codebase.** Churn picks where the work is, but the security and data lenses pick by entry points and trust boundaries: public endpoints, auth, payments, data stores. A payments core nobody has touched in years still matters.
- When coverage is bounded, record what was skipped in `run.json`, and report it in the sources and gaps section. A silent cap reads as "covered everything".
- **Multi-repo systems:** one architecture lens spans all repos, because it needs the joins. Other lenses run per repo, or span repos when the repos are small.

## Verification and wave 2

The workflow handles both:
- **Verification:** findings rated critical or high go to verifiers (`agents/verifier.md`) in batches of up to 8 per agent.
- **Wave 2:** docs readers (`agents/docs-reader.md`) take chunks of up to about 60k words of docs each, and only start after all verdicts are in, because they check docs against verified facts.

Numbers aren't verified by agents. Numbers come from the tool.
