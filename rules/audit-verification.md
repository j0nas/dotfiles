When auditing code or relaying findings from subagents, EVERY claim must be verified against the actual current state before being listed. Pattern-matching without verification produces false positives.

## Why

Real session example: across a single audit, four out of ~16 substantive claims turned out to be wrong — a 25% false-positive rate. The pattern was always the same: an audit agent pattern-matched on suspicious-looking code or stale training data without checking whether the claim held up, and I relayed those claims as fact into reports the user then acted on. Examples of what got through:

- A phantom `7,719 tsc errors` claim — actual count was 0.
- `lodash@4.18.1 doesn't exist` — actually a legitimate jdalton release.
- `OAuth bypass on lines X-Y` — the code matched the pattern but the methods were unreachable dead code, not live bypasses.
- `Stale build artifacts not gitignored` — all properly gitignored.

The user called it out as "sloppy researching."

## How to apply

- For any claim about a specific line or function existing/doing X: read the file at that line and confirm before listing.
- For any count (e.g. "47 files import X"): run `grep -c` / `wc -l` and use the actual number.
- For any "package X is abandoned / version Y doesn't exist": run `npm view X time --json` and check publish dates against today's date.
- For any "this code is broken / a security bypass": trace it to the actual call sites to confirm it's reachable. Dead code with bad bodies is not a live bug.
- When delegating to subagents, instruct them to show verification evidence (file:line, grep counts, publish dates) alongside every finding, and to tag findings as "verified" vs. "needs follow-up".
- Distinguish empirically-confirmed claims from speculative pattern-matches in the report itself. Don't blur the two.
