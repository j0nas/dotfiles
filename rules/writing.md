Writing prose with a person as its intended reader (PR/MR descriptions, commit bodies, messages, docs, READMEs, CVs, site copy): before the first draft, fetch https://en.wikipedia.org/wiki/Wikipedia:Signs_of_AI_writing and check the text against its Content, Language and grammar, and Style sections. The page is long, so with WebFetch keep reading with `offset` until you've covered Style. Once per task is enough, and replies in this chat don't need it. If the fetch fails, go by the list below and say so.

Avoid these tells too. Prose only.

- No reflexive three-part lists. Two or four items are fine.
- State what is, not what isn't. Cut opening negatives ("Feeding never force-pushes", "No config needed") and "not X, but Y" / "Y rather than X", unless the reader actually expects the opposite.
- Write for the eventual reader. Leave out anything they never saw, like earlier drafts or the conversation that produced the text.
- Name what the text relies on well enough for a reader new to the project: "Y brings back bug Z" only works if the text says what Z is (which bug, file, device or setting).
- Leave out internal product and project names the reader has no prior knowledge of, and say what the thing is ("the customer website", not "Min TT"). A public name can stay if the reader will know it or the text explains it.
- Prose over bold-header bullets, unless it's a reference list someone will scan.
- No emoji or `---` as decoration.
- Plain verbs (is, has, used, wrote). "Very" and "probably" are fine.
- No hedging disclaimers ("doesn't by itself prove...") unless the caveat changes what the reader does.
- Don't carry a joke or metaphor through every section.
- For real work, say what's untested or rough in this specific change. A caveat that would fit any project is filler.
