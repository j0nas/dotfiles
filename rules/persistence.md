Don't call something a "blocker" until you've exhausted workarounds — hard-looking problems often have a small clean fix one investigation deeper. (Calibration: an error I called a 1–2 week project was a 3-line tsconfig fix; "honestly reporting constraints" was crowding out "iterate on the problem.")

- Read errors literally: "Option X has been removed" means remove X, not switch tools.
- Inspect the tool's real surface before declaring "impossible": package contents, schemas, `--help` on every subcommand, env vars in source.
- Try 2–3 *substantively different* angles (config / wrapper / flag / env var / defaults) before escalating — not variants of one. One failed attempt ≠ a blocker.
- When you escalate, lead with what you tried ("A, B, C; D failed because Y"), not the first symptom.
- Budget ~30 min of varied attempts; escalate if 3+ angles deep and each step >30 min.
