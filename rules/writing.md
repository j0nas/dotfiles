Writing prose with a person as its intended reader (PR/MR descriptions, commit bodies, messages, docs, READMEs, CVs, site copy): check the draft against the tells below. Replies in this chat don't need it. Prose only. Several lines come from Wikipedia's field guide "Signs of AI writing" (read in full 2026-11-21, its Wikipedia-only items left out), so there's no need to fetch it.

- No reflexive three-part lists. Two or four items are fine.
- State what is, not what isn't. Cut opening negatives ("Feeding never force-pushes", "No config needed") and negative parallelisms ("not X, but Y", "not only X but also Y", "it's not X, it's Y", "no X, no Y, just Z", "Y rather than X"), unless the reader actually expects the opposite.
- Plain verbs (is, has, used, wrote) over "serves as", "stands as", "marks", "represents", "features", "offers", "boasts". "Very" and "probably" are fine.
- No inflated significance ("a pivotal moment", "underscores its importance", "a testament to", "setting the stage for", "has shaped conversations about") and no "-ing" tails that hand down a verdict ("highlighting its significance", "reflecting broader trends", "fostering…", "enhancing…").
- Watch for AI vocabulary; it tells when several gather in one text: delve, pivotal, crucial, underscore, showcase, tapestry, testament, enduring, intricate, meticulous, valuable, vibrant, garner, interplay, align with, bolster, foster, highlight (verb), robust, leverage, multifaceted, prioritize, quietly, resonate, insights.
- Name sources and links: no "associated with", "experts argue", "widely seen as", or "several sources" for one. No formulaic "Despite its success, X faces challenges…" paragraph ending in vague optimism.
- Write for the eventual reader. Leave out anything they never saw, like earlier drafts or the conversation that produced the text.
- Name what the text relies on well enough for a reader new to the project: "Y brings back bug Z" only works if the text says what Z is (which bug, file, device or setting).
- Leave out internal product and project names the reader has no prior knowledge of, and say what the thing is ("the customer website", not "Min TT"). A public name can stay if the reader will know it or the text explains it.
- Prose over bold-header bullets, unless it's a reference list someone will scan. Sentence case in headings, no bold scattered through prose, no emoji or `---` as decoration, straight quotes where the medium uses them.
- In commit messages and other change summaries, say what changed and why, specifically. Leave out canned assurances ("ensured", "refined", "streamlined", "for clarity", "improved flow") and lists of what was kept ("while preserving the original behaviour", "retained", "avoided"), and don't pour every message into one template.
- No hedging disclaimers ("doesn't by itself prove...") unless the caveat changes what the reader does.
- Don't carry a joke or metaphor through every section.
- For real work, say what's untested or rough in this specific change. A caveat that would fit any project is filler.
- Not tells on their own: correct grammar, formal prose, a transition word like "Additionally", a plain tone.
