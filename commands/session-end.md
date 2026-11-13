---
description: Close out the session log. Writes this session's entry in each project it changed, finishes every draft other sessions left unfinished, and brings what the session made stale up to date.
argument-hint: "[session id of a leftover draft]"
---

Close out the session log: this session's entries first, then every draft another session left unfinished, then whatever this session's work made stale.

Session id given, if any: $ARGUMENTS

A session id is another session whose draft was left unfinished. Do only that one: add `--session <id>` to the `llm-log` commands in steps 1 to 5 and write its entries from that draft's notes alone, since this session never saw that conversation. Then stop.

## This session

1. Run `llm-log show`. It prints the session's draft notes and any entries it already has, in every project it has logged in.
2. Work out which projects get an entry: each one `show` names, and any other repo this session changed, with or without notes. A project that was only read gets none, and neither does one whose notes all turned out to be about another. Decide this yourself rather than asking.
3. For each project, write the entry's body from the notes and from what this session did there, holding only what concerns that project. What spans projects goes where the change landed, with a one-line pointer in the other only if someone working there would need it. Name the other projects' entries in a closing line (`Also: touchup, 2026-11-05-<slug>`). Use these sections, leaving out any that would be empty:
   - `## Decisions`: what was chosen, why, and what was rejected.
   - `## Learnings`: gotchas and facts worth knowing next time.
   - `## Dead ends`: what was tried and why it failed.
   - `## Open`: what is unfinished or still to decide.
   - `## Closes`: items an earlier entry of this project lists as open that this session finished, each with that entry's slug. Earlier entries stay as written, so this line is how a reader learns the item is done.

   Write tersely and plainly, without flair: one line per bullet where possible, the reason as a clause rather than a sentence, and a pointer to a doc that already explains something instead of restating it. Each bullet has to make sense to a reader new to the project who never saw this session: name the bug, file, device or setting it refers to, and spell out what a term means if this session coined it. The file must stand on its own, since Claude Code deletes transcripts after 30 days. For a resumed session, keep the existing entry and add a `## <date>, resumed` heading with the new sections under it as `###`.
4. For each entry, choose a slug that names what the session did or decided (for example `wall-brackets-replace-rear-posts`), a title of at most 80 characters, and a one-sentence summary of at most 240 characters for the index: what changed or was decided, and what is left open.
5. From each project's folder, pipe the body to `llm-log finish --slug <slug> --title "<title>" --summary "<summary>"`, through a quoted heredoc (`<<'EOF'`) so the shell leaves the text alone: `cd <project> && llm-log finish … <<'EOF'`. It writes the file, adds or updates that project's INDEX.md line and deletes that project's draft, and it says which other projects still hold this session's notes. A draft in a project that gets no entry stays until you finish there, so make sure none is left. If it reports the slug as taken, choose another.

## Drafts other sessions left

A session that ends without this command leaves its draft behind, in any project, and nobody comes back for it.

6. Run `llm-log drafts --all`. Each line is `<project>: <session id> (started <date>, <n> notes, last note <age> ago)`, for every project a session of the last 30 days logged in.
7. Finish each one, unless its last note is under two hours old: that session may still be at work, so leave its draft and name it in your report.
   - `cd <project> && llm-log show --session <id>` prints its notes, in every project that session logged in. Give each of those projects its entry.
   - Write the entry from the notes alone. You never saw that conversation, so add nothing the notes don't say. The project's `git log` around the draft's dates can settle whether an open item was done since; say "open when the session ended" where you can't tell.
   - Finish it as in steps 4 and 5, with `--session <id>` on `llm-log finish`.
   - A draft with no note worth keeping gets no entry: delete its file in `<project>/llm-sessions/.drafts/`.

## What went stale

8. Look for what this session's work, or the entries just written, now contradicts, and correct it where it lives:
   - **Queued work.** `claude-quota queue list`: an item this session did, did part of or changed. The queue has no edit, so `drop` the item and `add` it again with the corrected note, and fix any other item's note that names the dropped id.
   - **Agents logged as running.** One this session started with `claude-quota start` and never ended gets its `end`.
   - **Memory.** A memory file of the project that this session's decisions contradict: update it or delete it, and its line in `MEMORY.md`.
   - **Things left running.** Watchers and monitors this session started, and worktrees whose branches are merged.
   - **Tracked files.** A work queue or docs page in a repo belongs to the project. Update it only where the project's rules have sessions keep it current, and commit as the project requires. Otherwise name it for the user.
9. Tell the user: each entry's path and index line, any draft left because its session may still be running, and what was stale, saying which of it you corrected and which needs them.
