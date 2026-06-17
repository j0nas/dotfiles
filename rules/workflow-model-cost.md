Before launching a multi-agent Workflow, check which model its agents use: they inherit the **session model** unless each `agent()` pins `model:`. High fan-out on an expensive session model can silently spawn ~100 Opus agents and blow a usage limit in one run, often returning nothing. (This happened: a deep-research workflow inherited Opus 4.8 across ~97 agents and blew a 5-hour limit.)

- Read the script's `agent()` opts. No `model:` → session model; estimate worst-case agent count (sum fan-out caps: MAX_FETCH, claims × votes, search angles…) × per-agent cost. Large product on Opus/Fable → pin models or confirm cost with the user first.
- Default tiering: Haiku 4.5 for high-volume mechanical phases (search/fetch/verify); Sonnet 4.6 for the few reasoning steps (scope/synthesize); Opus only when justified.
- IDs: Haiku 4.5 `claude-haiku-4-5-20251001`, Sonnet 4.6 `claude-sonnet-4-6`, Opus 4.8 `claude-opus-4-8`, Fable 5 `claude-fable-5`.
