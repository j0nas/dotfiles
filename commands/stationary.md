---
name: stationary
description: Operate my Windows desktop (the stationary) from this Mac over SSH — run commands, transfer files, pull from the media server.
argument-hint: "[what you want to do on the stationary]"
---

# Stationary

Read `~/.claude/secrets/stationary-notes.md` first, every time: how to connect, the security setup to keep, and how to run PowerShell, long jobs and transfers there.

If it's missing, this Mac isn't meant to reach the desktop (chezmoi writes it only where its key is in `.age-recipients`): say so and stop.

This file is public, and so are the dotfiles' commit messages. What you learn or change goes in the notes, never here, and their commits say only "stationary notes". To save the notes after an edit, then commit with `/dotfiles`:

```bash
chezmoi encrypt --output "$(chezmoi source-path ~/.claude/secrets/stationary-notes.md)" ~/.claude/secrets/stationary-notes.md
```

## Task: $ARGUMENTS

Work autonomously: discover, don't ask; the user never wants to touch PowerShell. Confirm before destructive actions (deleting files, tearing down the SSH setup, re-enabling password auth).
