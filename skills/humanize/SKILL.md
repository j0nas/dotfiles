---
name: humanize
description: Rewrite AI-sounding prose to read like a human wrote it. Use on MR descriptions, messages, docs, or any text that feels too polished.
user-invocable: true
---

Humanize the text the user provides (or the most recent prose you generated in this conversation). Rewrite it to sound like a person wrote it.

The signals below come from Wikipedia's "Signs of AI writing" field guide. Use them to detect; the rewriting rules at the bottom show how to fix each one.

## Core meta-pattern

LLM text simultaneously *underspecifies* (regresses facts into generic statements that could apply to any subject in the category) while *over-emphasizes* (puffery, superficial analysis, claims of significance). The result reads as both vague and grandiose. Cut in both directions: name specifics, drop the puffery.

## Vocabulary tells

AI overuses these. Density matters more than any single word. Clusters shift over time:

- **2023 to mid-2024:** delve, intricate, intricacies, testament, landscape (abstract), tapestry (abstract), vibrant, robust, meticulous, meticulously, pivotal, crucial, enduring, key (adj.)
- **Mid-2024 to mid-2025:** align with, enhance, highlighting, showcasing, fostering, garner, bolstered, boasts, underscore (verb)
- **Mid-2025 onward:** emphasizing, enhance, showcasing, plus notability-focused vocabulary
- **Always on:** additionally (sentence-starting), interplay, valuable, exemplifies, commitment to, groundbreaking, renowned, diverse array

## Promotional / press-release tone

Travel-guide and marketing language attached to mundane subjects:

- "boasts a", "nestled", "in the heart of", "natural beauty"
- "captivates", "charm", "backdrop"
- "commitment to sustainability", "responsible practices"
- "sleek", "refined dynamism"

## Sentence structure

### Copula avoidance
AI replaces "is/are/has" with status verbs: serves as, stands as, marks, represents, boasts, features, maintains, offers, refers to.

### Negative parallelisms
- "Not only X, but also Y"
- "Not X, but Y"
- "It's not [thing], it's [grander thing]"
- "No X. No Y. Just Z."

Creates a fake misconception to correct.

### Rule of three
Trinomial phrases appearing constantly: "adj, adj, adj", "phrase, phrase, and phrase". Three-item lists arriving unprompted.

### Elegant variation (synonym cycling)
A subject mentioned, then later called "the protagonist", "the key player", "the eponymous character". AI avoids repeating nouns.

### Superficial "-ing" tails
"...highlighting / underscoring / emphasizing / ensuring / reflecting / symbolizing / contributing to / cultivating / fostering / encompassing..."

Present participles tacked onto claims to simulate analysis. Usually paired with vague attribution.

## Content patterns

### Significance / legacy inflation
- "stands as a vital", "marks a pivotal moment"
- "testament to", "underscores the importance of"
- "reflects broader trends", "evolving landscape"
- "symbolizing ongoing/enduring/lasting"
- "setting the stage for", "indelible mark"
- "contributing to the [abstract noun]"

Applied even to mundane subjects (etymology, population data).

### "Challenges and Future Prospects" formula
Closing structure: "Despite [positive words], [subject] faces challenges... With [ongoing initiatives], [subject] continues to [thrive/evolve]..."

### Vague attribution / weasel wording
- "Industry reports", "Observers have cited", "Experts argue"
- "Researchers have shown", "Scholarship describes"
- "Multiple reviewers/scholars" while citing only one
- "such as" before a small list pretending to be exhaustive

### Notability name-dropping
Lists of outlets the subject was "featured in", "profiled in", with phrases like "independent coverage", "local/regional/national media outlets", and no specific claim attached.

### Awkward definitional leads
Treating non-entity topics as proper nouns: "**[Topic]** refers to...", "**[Topic]** is..." for lists, broad subjects, or things that are not entities.

## Punctuation and typography

- **Em-dash overuse.** Used in place of commas or periods, often inappropriately.
- **Curly / smart quotes** ("text") and apostrophes instead of straight ones.
- **Title Case In Headings** capitalizing every main word.
- **Boldface overuse.** Every chosen phrase bolded; entire phrases bold for fake importance.
- **Unicode symbols** where ASCII would do: arrow instead of ->, ellipsis instead of ..., en/em dashes instead of --.

## Formatting tics

### Inline-header bullet lists
Bulleted lines of the form `• **Header:** descriptive text`. Listicle / readme aesthetic dropped into prose.

### Markup that does not belong
- Markdown emphasis (`*italics*`, `**bold**`) inside hosts that use other markup (e.g. wikitext)
- Thematic breaks (`---`) before headings
- Skipped heading levels (H2 jumping to H4)
- Numbered lists with explicit "1." instead of host-native numbering
- Emoji as list markers (🔹, 📌, ➡️)

## Markup leak artifacts

If any of these strings appear, the text was pasted from an LLM UI without cleanup:

- `contentReference`
- `oaicite`, `oai_citation`
- `turn0search0`, `+1`, `attached_file`, `grok_card`
- `attribution`, `attributableIndex`

Citations may also have malformed DOIs, invalid ISBNs, dead URLs, or `utm_source` parameters.

## Communication / chatbot residue

- "I hope this helps", "Let me know if..."
- "Great question", "You're absolutely right"
- "I appreciate the feedback", "open to constructive criticism"
- Knowledge-cutoff disclaimers: "As of my last training data...", "While details are limited..."
- Prompt-refusal text: "I can't fulfill that request"
- Abrupt mid-sentence cutoffs

## Structural patterns

- Rigid Overview to Details to Challenges to Future hierarchy
- Section summaries that recap what the reader just read
- Pronounced style shifts mid-document (a section was AI-copyedited)
- Statistical regression to the mean: every specific fact smoothed into a generic statement that could apply to any topic in the category

## What is NOT a reliable signal

Do not flag text as AI on these alone:

- Perfect grammar
- Heavy citations
- Professional terminology
- Length

Automated detectors (e.g. GPTZero) beat random chance but have non-trivial error rates. A 2025 preprint found heavy LLM users identify AI-generated articles at roughly 90% accuracy by inspection. Trust the patterns above plus your own read.

## Rewriting rules

### Cut both directions of the meta-pattern
- Replace generic claims with one specific fact.
- Delete claims of significance unless you can name a concrete consequence.

### Restore plain syntax
- Use "is" and "has". Status verbs ("serves as", "boasts") are pretentious.
- One qualifier per claim. Don't stack hedges.
- Repeat nouns. Don't synonym-cycle.
- Drop trailing "-ing" analyses. Replace with a concrete consequence or remove.

### Source or drop
- Name the source, or cut the claim.
- Cut name-drop lists of outlets unless you quote what they said.

### Strip the formatting tics
- Em-dash to comma, period, or parentheses.
- Smart quotes to straight quotes.
- Title Case to sentence case.
- ASCII over Unicode in casual writing: `->` not arrow, `--` not en/em dash, `...` not ellipsis.
- Inline-header bullet blocks to flowing paragraphs (unless a list genuinely fits).
- Strip every leak artifact (`contentReference`, `oaicite`, etc.).

### Cut the residue
- Delete chatbot pleasantries, sycophancy, training-cutoff disclaimers, prompt-refusal phrases.
- Delete the "Despite challenges... continues to evolve" closing paragraph entirely. End on a specific detail.

### Add a person
- Have a view. React, don't only report.
- Vary sentence length. Short. Then a longer one that wanders a little.
- Acknowledge mixed feelings or open questions.
- Let some mess in. Perfect structure feels algorithmic.

## Process

1. Read the input.
2. Scan for the patterns above. Note which fire.
3. Rewrite. For each flagged pattern, apply the matching rule from the rewriting section.
4. Preserve meaning and match the intended register (formal, casual, technical).
5. Read the result silently. If a sentence still sounds like a press release or a Wikipedia stub, rewrite it again.
6. Return the rewrite with a short note on the main patterns you removed.
