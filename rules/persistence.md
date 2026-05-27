Don't escalate a "blocker" finding to the user until you've actually exhausted reasonable workarounds. Hard-looking problems often have small clean solutions one investigation deeper.

When a tool errors or a setup looks impossible, the default impulse to report it as a structural blocker and propose pulling back scope is **usually wrong if you've only tried one angle**. Iterate on the problem before escalating. The user is calibrated for someone who tries hard before saying "can't."

## Why

This rule was born from real sessions where I called something "blocked" prematurely and the user had to push back to get me to keep trying.

- An oxlint + typescript-go experiment hit `Option 'moduleResolution=node10' has been removed`. The first reaction was to report it as blocked by a 1-2 week debt-modernization project and propose reverting to ESLint. Pushed by the user to try harder, I found: (a) a working wrapper-script workaround in 15 minutes, then (b) a much simpler 3-line tsconfig change that resolved everything without any workaround. The outcome was strictly better than the original ESLint plan — 12× faster lint, more rule coverage, simpler config. The "blocker" was 30 minutes of debugging away.
- Same session, the `moduleResolution: node10` → modern upgrade was called "blocked, ~1-2 weeks of work" after one failed `module: node16` attempt. The actual fix (drop the explicit `node10`, keep the implicit default) was a single-line change.

Twice in one session, "blocker" was wrong. The pattern: over-indexing on "honestly report constraints" at the expense of "iterate on the problem."

## How to apply

- When a tool reports an error, first read the message **literally**: "Option X has been removed" usually means "remove option X," not "use a different tool."
- Inspect the tool's actual surface before reporting "no way to do this." Read package contents (`node_modules/<pkg>/`), schema files, `--help` for every subcommand, env vars referenced in source. Modern tooling often has undocumented flags or escape hatches.
- Try at least 2-3 substantively different angles of attack before reporting a finding as blocked. Each angle should be substantively different — different config, wrapper script, alternate flag, env var, different defaults — not variations of the same approach.
- Distinguish "I tried one thing and it didn't work" (not yet a blocker — try variants) from "I tried 3 different angles and they all hit the same root issue" (real blocker).
- When the user has invested in an idea ("test if X works"), assume they want you to **actually find a way** — not to scope-protect them from complexity.
- When you DO report a blocker, lead with the experiments tried, not just the symptom. "I tried A, B, C, and D failed because Y" is what proves it's blocked. "X errors with this message" is just the first symptom.
- Persistence has a budget. If you're 3+ angles deep and each step takes >30 min, escalate. The default budget for hard-looking-but-likely-tractable problems is roughly "30 minutes of varied attempts before pulling back."
