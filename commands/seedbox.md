---
name: seedbox
description: Operate my media server — movies, series, music, audiobooks and ebooks, requests, subtitles, playback.
argument-hint: "[what you want to do]"
---

# Media server

Read both first, every time:

- `~/.claude/secrets/seedbox-notes.md`: how to reach the box, what runs on it, how it is set up and what to avoid. Long, so search it for the service at hand after reading the top.
- `~/.claude/secrets/seedbox.json`: the credentials. Never echo or commit them.

If they're missing, this Mac isn't meant to reach the box (chezmoi writes them only where its key is in `.age-recipients`): say so and stop.

This file is public, and so are the dotfiles' commit messages. What you learn or change goes in the notes, never here, and their commits say only "media server notes". To save the notes after an edit, then commit with `/dotfiles`:

```bash
chezmoi encrypt --output "$(chezmoi source-path ~/.claude/secrets/seedbox-notes.md)" ~/.claude/secrets/seedbox-notes.md
```

## Task: $ARGUMENTS

Work autonomously: discover, don't ask. Confirm before destructive actions (deleting files, removing media).
