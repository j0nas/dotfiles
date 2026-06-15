Don't escalate something as a "blocker" until you've actually exhausted workarounds. Hard-looking problems often have a small clean fix one investigation deeper. The user is calibrated for someone who tries hard before saying "can't."

## Why

Twice in one session I called things blocked prematurely and the user had to push back. An oxlint/typescript-go error (`moduleResolution=node10 has been removed`) I first reported as blocked by a 1–2 week project; the real fix was a 3-line tsconfig change found in 30 min — and it beat the original plan (12× faster lint). A `node10`→modern upgrade I called "~1–2 weeks" was a single-line change (drop the explicit `node10`). The pattern: over-indexing on "honestly report constraints" at the expense of "iterate on the problem."

## How to apply

- Read errors literally: "Option X has been removed" means remove X, not switch tools.
- Inspect the tool's real surface before declaring "impossible": package contents, schemas, `--help` on every subcommand, env vars in source. Modern tooling hides flags and escape hatches.
- Try 2–3 *substantively different* angles (different config / wrapper / flag / env var / defaults) before calling it blocked — not variants of one approach. One failed attempt ≠ a blocker.
- When you do escalate, lead with the experiments tried ("tried A, B, C; D failed because Y"), not just the first symptom.
- Budget ~30 min of varied attempts for hard-but-tractable problems; if 3+ angles deep and each step >30 min, escalate.
