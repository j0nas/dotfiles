---
description: Write this session's entry in the llm-sessions log of each project it changed, or finish a leftover draft given its session id.
argument-hint: "[session id of a leftover draft]"
---

Write this session's entries in the session logs, one for each project it changed.

Session id given, if any: $ARGUMENTS

A session id is another session whose draft was left unfinished: add `--session <id>` to the `llm-log` commands below and write its entries from that draft's notes alone, since this session never saw that conversation.

1. Run `llm-log show`. It prints the session's draft notes and any entries it already has, in every project it has logged in.
2. Work out which projects get an entry: each one `show` names, and any other repo this session changed, with or without notes. A project that was only read gets none, and neither does one whose notes all turned out to be about another. Decide this yourself rather than asking.
3. For each project, write the entry's body from the notes and from what this session did there, holding only what concerns that project. What spans projects goes where the change landed, with a one-line pointer in the other only if someone working there would need it. Name the other projects' entries in a closing line (`Also: touchup, 2026-11-05-<slug>`). Use these sections, leaving out any that would be empty:
   - `## Decisions`: what was chosen, why, and what was rejected.
   - `## Learnings`: gotchas and facts worth knowing next time.
   - `## Dead ends`: what was tried and why it failed.
   - `## Open`: what is unfinished or still to decide.

   Write tersely and plainly, without flair: one line per bullet where possible, the reason as a clause rather than a sentence, and a pointer to a doc that already explains something instead of restating it. Each bullet has to make sense to a reader new to the project who never saw this session: name the bug, file, device or setting it refers to, and spell out what a term means if this session coined it. The file must stand on its own, since Claude Code deletes transcripts after 30 days. For a resumed session, keep the existing entry and add a `## <date>, resumed` heading with the new sections under it as `###`.
4. For each entry, choose a slug that names what the session did or decided (for example `wall-brackets-replace-rear-posts`), a title of at most 80 characters, and a one-sentence summary of at most 240 characters for the index: what changed or was decided, and what is left open.
5. From each project's folder, pipe the body to `llm-log finish --slug <slug> --title "<title>" --summary "<summary>"`, through a quoted heredoc (`<<'EOF'`) so the shell leaves the text alone: `cd <project> && llm-log finish … <<'EOF'`. It writes the file, adds or updates that project's INDEX.md line and deletes that project's draft, and it says which other projects still hold this session's notes. A draft in a project that gets no entry stays until you finish there, so make sure none is left. If it reports the slug as taken, choose another.
6. Tell the user each file's path and its index line.
