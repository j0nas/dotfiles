# Factory readiness

"Software factory" describes the 2026 practice of agents turning specs into merged, shipped code, where correctness comes from deterministic checks and holdout tests instead of a human reading every line. The field moves fast and the models below will date. Use them as references that can be swapped out, and keep the section to one screen.

The section shows two numbers, and the headline is the gap between them.

## Autonomy: how much of the work agents do

Measure from git (`snapshot.git`) and, with forge access, from PRs:
- share of commits with agent co-author trailers or bot authors, per month;
- share of PRs opened by agents;
- share of agent PRs merged without a human commit added.

Map the result to Dan Shapiro's five levels (January 2026), which describe the human's role, not the output's quality:

| Level | Human role |
|---|---|
| 0–1 | writes the code, with autocomplete or chat help |
| 2 | pairs with an agent on bounded tasks |
| 3 | full-time reviewer of agent-written code |
| 4 | writes specs and manages agents, reviewing outcomes |
| 5 | dark factory: specs in, software out, no human code review |

## Harness strength: how much automated checking backs that work

Score from the baseline, not from opinion. The pillars are adapted from Factory.ai's Agent Readiness model (five levels, each unlocked at 80% of the previous level's criteria):

| Pillar | Example criteria (each one a yes or no from the baseline) |
|---|---|
| style and validation | linter and type checker exist; zero type errors; lint blocks merges |
| build | builds from a clean export with documented, dummy-only config |
| testing | tests run in CI; pass rate; no flaky tests; integration or end-to-end tests exist |
| docs | agent instructions exist and are accurate (wave 2); drift rate is low |
| dev environment | one-command setup works; env example complete |
| observability | error tracking wired; alerts configured; logs free of PII |
| security | secret scanning; dependency audit in CI; no critical advisories |
| task discovery | issues and briefs are the spec; tests encode acceptance |
| product | feature flags, analytics, ways to roll back |

Levels:

| Level | Name |
|---|---|
| 1 | functional |
| 2 | documented |
| 3 | standardised |
| 4 | optimised |
| 5 | autonomous |

Show the pillar table with each criterion met or unmet and its evidence, then the level.

## The gap

When autonomy runs ahead of harness strength, agents are shipping faster than anything can check them, and slop and security holes accumulate. "Dark without a strong oracle just means unreviewed." Say how large the gap is and which three harness items would close most of it. These usually overlap with the top systemic fixes in the plan.

A working, earning product can sit at any point on both axes. Readiness describes how safely the current way of working can continue or speed up. It says nothing about whether the product is good.
