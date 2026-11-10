# What the reader asked for (quoted from them)

The reader is an experienced engineer who just cloned an unfamiliar codebase, possibly joining as CTO or contractor. Their own words:

- Goal 0: "break down the codebase in question to someone who's just cloned the repo(s) and have never been introduced to it before."
- Goal 1, "above all": "how to establish a baseline in terms of whatever data we can capture, so we empirically can know whether our following actions improve or worsen the situation over time (and in the case of me being an external/hired contributor, to validate my contributions over time)."
- Goal 2: "a breakdown of the highest-impact changes we could do and areas we could focus on ... with focus on improving an established baseline."
- Focus: "the technical side of things", domain "only when it makes sense".
- Terse and visual: "use rich media instead of text (svg, mermaid, what have you) ... as effectively and skimmable as possible." "I should be able to scan it on a bad day and not feel a great sense of overwhelm."
- No borrowed context: "the Claudeism ... of assuming the reader will somehow magically be able to share the context that the current session has ... e.g. the sudden references to 'R3'/'R3.3b' that don't explain what that refers to."
- Opinions: "The 'My take' addendums are valuable. The takes where you see the potential to _systematically_ fix/improve on recurring issues that e.g. have been fixed on an ad-hoc basis are doubly so."
- Docs: rate them on accuracy and unique value, not volume; drift is a risk.
- Measurement is a guiding principle, not dogma: obvious fixes needn't wait for metrics.

# Scoring

Score each report 1-5 on each criterion, with one line of justification citing the page:
1. breakdown_for_newcomer  2. baseline_quality (re-measurable numbers, each with how it was measured)
3. impact_plan_tied_to_baseline  4. terse_visual_skimmable  5. no_borrowed_context
6. takes_and_systemic_fixes  7. accuracy

Also count, per report:
- claims_checked / claims_wrong: spot-check at least 15 concrete claims per report (numbers, file paths, line refs, findings) against the repo, read-only. List every wrong one with the quoted claim and what the repo shows.
- undefined_terms: terms, labels, codenames, ticket ids or names a newcomer couldn't define from the page. List with quotes.
- session_leaks: text referring to a conversation, plan or context the reader never saw. List with quotes.
- fabricated_or_unsupported: findings with no basis in the code you can find.
- reproducible_metrics: number of metrics that state how to re-measure them.
- recs_tied_to_metric: recommendations that name the metric they move / total recommendations.
- systemic_takes: opinions proposing a guard that stops a class of problem.

Finish with: preferred report (A or B) and the three biggest differences. Return JSON only:
{"A": {"scores": {...}, "justifications": {...}, "counts": {...}, "lists": {...}}, "B": {...}, "preferred": "A|B", "differences": ["..."]}
