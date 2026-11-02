Keep a session log with `llm-log` (`llm-log --help`). Each project has one in `llm-sessions/` at its root: a file per session and `INDEX.md`, one line per session, newest first. It is local only and never committed.

- Note things as they happen: `llm-log note <decision|learning|dead-end|open> "<text>"`. A decision says what was chosen and why. An `open` note is something left unfinished. One line each, self-contained: name the bug, file, device or setting it refers to, since a later reader never saw this session. Leave out the play-by-play; the diff has it. A session that changes and decides nothing logs nothing.
- Only the main session logs. A subagent or worktree agent reports to its parent instead.
- When the work reaches a natural end (done and verified, committed, or the user is wrapping up), suggest `/session-end`.
- Read the log only when the user refers to earlier work, or before you debug a failure or change a design choice. Grep the `INDEX.md` in the folder `llm-log path` prints, with the search pointed at that folder (a search from the project root skips it, since it is gitignored), and open a session file only when a line matches.
