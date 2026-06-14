Before launching any multi-agent Workflow, verify which model its spawned agents will use. Workflow agents inherit the current **session model** unless the script pins an explicit per-agent `model:` on each `agent()` call. A high-fan-out workflow launched while the session model is Opus can silently spawn ~100 Opus agents and burn through a usage limit (e.g. a 5-hour cap) in a single run — often without returning any result.

## Why

Real session: the user set their session model to Opus 4.8 via `/model`, then asked for a deep-research run. The deep-research workflow script had ZERO model overrides on any `agent()` call, so every agent inherited the session model = Opus 4.8. The script's worst-case fan-out is ~97 agents: 1 scope + 5 search + up to 15 fetch (each `WebFetch`-ing full page content) + up to 75 verify (25 claims × 3 votes, each running its own `WebSearch`) + 1 synthesize. That's ~97 Opus agents, ~90 of them pulling whole web pages or running searches — which blew the user's 5-hour usage limit with no result returned. A previous run had already done the same thing.

The fix was to pin Haiku 4.5 on the ~95 high-fan-out search/fetch/verify agents and Sonnet 4.6 on only the 2 reasoning-heavy steps (scope, synthesize) — dropping cost roughly an order of magnitude while leaving the 3-vote verification quorum intact for quality. The cost wasn't the model alone; it was model × fan-out, and nobody checked the multiplier before launching.

## How to apply

- Before calling the Workflow tool, determine what model the agents will run on: read the script's `agent()` opts. If there is no explicit `model:`, the agents inherit the session model — check what that currently is.
- If the session model is expensive (Opus / Fable) and the script has no per-agent model pins, EDIT the script to pin models before launching, or explicitly confirm with the user that the cost is acceptable.
- Estimate worst-case agent count by summing the fan-out caps (`MAX_FETCH`, `MAX_VERIFY_CLAIMS` × votes-per-claim, number of search angles, etc.) and multiply by per-agent model cost. If that product is large and on an expensive model, fix the models first.
- Default tiering for research-style workflows: cheap fast model (Haiku 4.5) for the high-volume mechanical phases (search, fetch/extract, adversarial verify — dozens of agents); reserve the stronger model (Sonnet 4.6, or Opus only when justified) for the few low-count reasoning steps (scope/decompose, synthesize/report).
- This applies to deep-research and ANY multi-agent workflow that fans out — never launch a fan-out workflow on an expensive session model without checking the per-agent model first.
- Model IDs for reference: Haiku 4.5 = `claude-haiku-4-5-20251001`, Sonnet 4.6 = `claude-sonnet-4-6`, Opus 4.8 = `claude-opus-4-8`, Fable 5 = `claude-fable-5`.
