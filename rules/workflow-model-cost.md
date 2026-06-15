Before launching any multi-agent Workflow, check which model its agents will use. Agents inherit the **session model** unless each `agent()` call pins an explicit `model:`. A high-fan-out workflow on an expensive session model can silently spawn ~100 Opus agents and blow a usage limit in one run — often returning nothing.

## Why

Session model was Opus 4.8; a deep-research workflow with zero per-agent `model:` overrides inherited it. Worst-case fan-out ~97 agents (1 scope + 5 search + ≤15 fetch + ≤75 verify [25 claims × 3 votes] + 1 synthesize), ~90 of them pulling whole pages or searching — blew a 5-hour limit with no result. Cost is model × fan-out; nobody checked the multiplier.

## How to apply

- Before calling Workflow, read the script's `agent()` opts. No `model:` → agents use the session model; check what it currently is.
- Estimate worst-case agent count (sum the fan-out caps: MAX_FETCH, claims × votes, search angles…) × per-agent model cost. Large product on Opus/Fable → pin models first or confirm the cost with the user.
- Default tiering: Haiku 4.5 for high-volume mechanical phases (search, fetch, adversarial verify); Sonnet 4.6 for the few reasoning steps (scope, synthesize); Opus only when justified.
- Model IDs: Haiku 4.5 `claude-haiku-4-5-20251001`, Sonnet 4.6 `claude-sonnet-4-6`, Opus 4.8 `claude-opus-4-8`, Fable 5 `claude-fable-5`.
