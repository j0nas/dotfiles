---
description: Write this session's entry in the project's llm-sessions log, or finish a leftover draft given its session id.
argument-hint: "[session id of a leftover draft]"
---

Write this session's entry in the project's session log.

Session id given, if any: $ARGUMENTS

A session id is another session whose draft was left unfinished: add `--session <id>` to both `llm-log` commands below and write the entry from that draft's notes alone, since this session never saw that conversation.

1. Run `llm-log show`. It prints the session's draft notes and, for a resumed session, the entry it already has.
2. Write the entry's body from those notes and from what this session did, in these sections, leaving out any that would be empty:
   - `## Decisions`: what was chosen, why, and what was rejected.
   - `## Learnings`: gotchas and facts worth knowing next time.
   - `## Dead ends`: what was tried and why it failed.
   - `## Open`: what is unfinished or still to decide.

   Write tersely and plainly, without flair: one line per bullet where possible, the reason as a clause rather than a sentence, and a pointer to a doc that already explains something instead of restating it. Each bullet has to make sense to a reader new to the project who never saw this session: name the bug, file, device or setting it refers to, and spell out what a term means if this session coined it. The file must stand on its own, since Claude Code deletes transcripts after 30 days. For a resumed session, keep the existing entry and add a `## <date>, resumed` heading with the new sections under it as `###`.
3. Choose a slug that names what the session did or decided (for example `wall-brackets-replace-rear-posts`), a title of at most 80 characters, and a one-sentence summary of at most 240 characters for the index: what changed or was decided, and what is left open.
4. Pipe the body to `llm-log finish --slug <slug> --title "<title>" --summary "<summary>"`, through a quoted heredoc (`<<'EOF'`) so the shell leaves the text alone. It writes the file, adds or updates the session's INDEX.md line and deletes the draft. If it reports the slug as taken, choose another.
5. Tell the user the file's path and its index line.
