# What the baseline covers

A baseline metric earns its place if all three hold:
- a tool can re-measure it on a later commit, with nobody's judgement involved;
- someone would act differently if it moved;
- it is hard to improve without the underlying thing improving.

Counts that are easy to game (lines of code, number of tests) are context, not targets. Pair them with something that resists gaming: pass rate with test count, warning count with whether lint blocks a merge.

## Feedback loops (always)

A feedback loop is anything that tells a developer or an agent that a change is wrong. List them fastest first:
1. type checker
2. linter and formatter check
3. unit tests
4. build
5. integration and end-to-end tests
6. CI pipeline
7. preview deploys
8. production error tracking and alerts

These loops are the harness an agent-driven team runs on. Their speed and enforcement matter as much as whether they exist.

For each loop, record:

| Field | How |
|---|---|
| exists | `discover` finds it in manifests, Makefiles, CI config |
| runs from a clean export | `run-loops` exit code, with dummy env only |
| duration | `run-loops` |
| result | parsed counts: tests passed/failed/skipped, type errors, lint warnings |
| flaky | test loops run twice; counts differ |
| enforced | CI runs it, and (with forge access) branch protection requires it; otherwise it depends on someone remembering |
| not run | the reason: needs a database, browser, secrets or a service |

A loop that exists but isn't enforced is the most common gap, and usually the cheapest systemic fix. Record it explicitly.

## Code (always)

From `measure` and `hotspots`:
- size per language;
- complexity;
- generated and minified files (excluded);
- the files that change most, and complexity × churn hotspots;
- direct dependency count per manifest;
- lockfiles;
- TODO and FIXME counts;
- test files per language.

Where it's cheap, add bundle or artefact size for UI projects, from the build output of a loop that already ran.

## Security (always)

- Dependency advisories: run the ecosystem's audit command as a loop in the run export (`pnpm audit --json`, `pip-audit`, `govulncheck ./...`, `cargo audit`, `bundle audit`) when it works offline or against the public registry.
- Secret scanning of history: `measure` uses gitleaks or trufflehog if installed. Otherwise wave 1's security lens greps for key patterns and reports that only a partial scan happened.
- Counts of risky patterns found by the security lens: unauthenticated endpoints, raw HTML injection, user-controlled fetches, string-built SQL. Present these as counts with file:line lists, so a later run can recount them.

## Delivery (always, from git; richer with forge access)

From `measure`:
- commits per month;
- contributors, top-author share, and how many authors cover 50% and 80% of commits (bus factor);
- share of commits with agent co-author trailers or bot authors, per month;
- merges, reverts, fix-commit share, median commit size.

With GitHub, GitLab or another forge:
- PR count and size;
- time to merge;
- review latency;
- share of agent PRs merged without human commits added;
- CI duration and failure rate;
- deploy frequency.

## Production (only with access)

Error rate and top errors, latency and real-user speed, uptime, usage (active users, peak load), deploy frequency and failed deploys. Add cost only if the user wants it. When cost comes from a billing dashboard, take it from invoices and subtract included credits, because usage pages show gross usage, not the bill.

## Backfill

`history` exports one commit per month for the past 12 months and records size, test files, dependency counts and TODOs. It executes nothing, so it never fails on a broken old build. It gives the reader a trend from day one. Don't backfill loop results: old commits rarely build cleanly with today's toolchain, and the failures would be noise.

## What needs the user

The model can find and measure all of the above. Three things are the user's call:
1. **Which 3–5 metrics are headline.** These are what the people paying would recognise as better or worse. Recommend some after the first report, and let the user pick.
2. **Which numbers contradict their gut.** A flagged number is either a measurement bug or a real surprise.
3. **Access.** Ask for it, saying which numbers each grant would add.
