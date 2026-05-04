---
name: humanize
description: Rewrite AI-sounding prose to read like a human wrote it. Use on MR descriptions, messages, docs, or any text that feels too polished.
user-invocable: true
---

Humanize the text the user provides (or the most recent prose you generated in this conversation). Rewrite it to sound like a specific person wrote it, not like it was extruded from a language model.

## The patterns to detect and fix

| #   | Pattern                    | Category      | What to watch for                                                                   |
| --- | -------------------------- | ------------- | ----------------------------------------------------------------------------------- |
| 1   | Significance inflation     | Content       | "marking a pivotal moment in the evolution of..."                                   |
| 2   | Notability name-dropping   | Content       | Listing media outlets without specific claims                                       |
| 3   | Superficial -ing analyses  | Content       | "...showcasing... reflecting... highlighting..."                                    |
| 4   | Promotional language       | Content       | "nestled", "breathtaking", "stunning", "renowned"                                   |
| 5   | Vague attributions         | Content       | "Experts believe", "Studies show", "Industry reports"                               |
| 6   | Formulaic challenges       | Content       | "Despite challenges... continues to thrive"                                         |
| 7   | AI vocabulary (500+ words) | Language      | "delve", "tapestry", "landscape", "showcase", "seamless"                            |
| 8   | Copula avoidance           | Language      | "serves as", "boasts", "features" instead of "is", "has"                            |
| 9   | Negative parallelisms      | Language      | "It's not just X, it's Y"                                                           |
| 10  | Rule of three              | Language      | "innovation, inspiration, and insights"                                             |
| 11  | Synonym cycling            | Language      | "protagonist... main character... central figure..."                                |
| 12  | False ranges               | Language      | "from the Big Bang to dark matter"                                                  |
| 13  | Em dash overuse            | Style         | Too many — dashes — everywhere                                                      |
| 14  | Boldface overuse           | Style         | **Mechanical** **emphasis** **everywhere**                                          |
| 15  | Inline-header lists        | Style         | "- **Topic:** Topic is discussed here", "- **Bold label.** Followed by explanation" |
| 16  | Title Case headings        | Style         | Every Main Word Capitalized In Headings                                             |
| 17  | Emoji overuse              | Style         | Decorating professional text with emoji                                             |
| 18  | Curly quotes               | Style         | "smart quotes" instead of "straight quotes"                                         |
| 19  | Unicode symbols            | Style         | → instead of ->, — instead of --, … instead of ...                                  |
| 20  | Chatbot artifacts          | Communication | "I hope this helps!", "Let me know if..."                                           |
| 21  | Cutoff disclaimers         | Communication | "As of my last training...", "While details are limited..."                         |
| 22  | Sycophantic tone           | Communication | "Great question!", "You're absolutely right!"                                       |
| 23  | Filler phrases             | Filler        | "In order to", "Due to the fact that", "At this point in time"                      |
| 24  | Excessive hedging          | Filler        | "could potentially possibly", "might arguably perhaps"                              |
| 25  | Generic conclusions        | Filler        | "The future looks bright", "Exciting times lie ahead"                               |

## Statistical signals to watch for

| Signal                    | Human          | AI            | Why                                          |
| ------------------------- | -------------- | ------------- | -------------------------------------------- |
| Burstiness                | High (0.5-1.0) | Low (0.1-0.3) | Humans write in bursts; AI is metronomic     |
| Type-token ratio          | 0.5-0.7        | 0.3-0.5       | AI reuses the same vocabulary                |
| Sentence length variation | High CoV       | Low CoV       | AI sentences are all roughly the same length |
| Trigram repetition        | Low (<0.05)    | High (>0.10)  | AI reuses 3-word phrases                     |

## Vocabulary to ban or flag

**Tier 1 (dead giveaways):** delve, tapestry, vibrant, crucial, comprehensive, meticulous, embark, robust, seamless, groundbreaking, leverage, synergy, transformative, paramount, multifaceted, myriad, cornerstone, reimagine, empower, catalyst, invaluable, bustling, nestled, realm

**Tier 2 (suspicious in density):** furthermore, moreover, paradigm, holistic, utilize, facilitate, nuanced, illuminate, encompasses, catalyze, proactive, ubiquitous, quintessential

**Phrases:** "In today's digital age", "It is worth noting", "plays a crucial role", "serves as a testament", "in the realm of", "delve into", "harness the power of", "embark on a journey", "without further ado"

## Rewriting rules

### Write like a person, not a press release

- Use "is" and "has" freely. "serves as" is pretentious.
- One qualifier per claim. Don't stack hedges.
- Name sources or drop the claim.
- End with something specific, not "the future looks bright".

### Add personality

- Have opinions. React to things, don't just report them.
- Vary sentence rhythm. Short. Then longer ones that meander a bit.
- Acknowledge complexity and mixed feelings.
- Let some mess in. Perfect structure feels algorithmic.

### Use ASCII, not Unicode

- `->` not `→`, `--` not `—`, `...` not `…`, `"` not `""`
- People don't look up special characters when writing casually

### Cut the fat

- "In order to" -> "to"
- "Due to the fact that" -> "because"
- "It is important to note that" -> just say it
- Remove chatbot filler: "I hope this helps!", "Great question!"

## Process

1. Read the input text
2. Scan for the 24 patterns and flag what you find
3. Check for statistical tells (uniform sentence length, low burstiness, repetitive phrasing)
4. Rewrite problematic sections
5. Preserve core meaning and match the intended tone (formal, casual, technical)
6. Verify it sounds natural if read aloud
7. Present the rewrite with a short summary of what you changed and why
