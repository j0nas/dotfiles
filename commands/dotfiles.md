---
name: dotfiles
description: Make an environment change reproducible — route the change to the right file in the chezmoi dotfiles repo, apply the repo's conventions, and commit + push.
argument-hint: "[change you want to make, or empty to catch up on dirty state]"
---

You are working in the user's chezmoi dotfiles repo at `~/.local/share/chezmoi`. Read `~/.local/share/chezmoi/CLAUDE.md` first if you haven't already this session — it documents the conventions you must follow.

## Two modes

### Forward mode (argument provided)
The argument describes a change the user wants persisted across machines.

1. **Route** the change to the right file/directory. Common patterns:
   - shell alias → `dot_config/zsh/aliases.zsh.tmpl`
   - Homebrew brew or cask, flatpak, winget package, or VS Code extension → `.chezmoidata.yaml`
   - new personal skill → `skills/<name>/SKILL.md` + `dot_claude/skills/symlink_<name>.tmpl` (contents `{{ .chezmoi.sourceDir }}/skills/<name>`)
   - new slash command → `commands/<name>.md` + `dot_claude/commands/symlink_<name>.md.tmpl` (contents `{{ .chezmoi.sourceDir }}/commands/<name>.md`)
   - secret → `chezmoi edit-encrypted dot_claude/secrets/<file>.json`
   - macOS default → `run_onchange_macos-defaults.sh.tmpl`
   - one-time setup task → `run_once_<name>.sh.tmpl` (alphabetical sort matters)
   - re-runs on content change → `run_onchange_<name>.sh.tmpl`
2. **Apply repo rules** from CLAUDE.md: idempotency, no hardcoded usernames, OS gating with `{{ if eq .chezmoi.os "darwin" -}}`, shared data via `.chezmoi.toml.tmpl` / `.chezmoidata.yaml`.
3. **Make the edit** in the chezmoi source dir.
4. **Verify** with `chezmoi apply` and read the output for errors.
5. Proceed to **Commit**.

### Catch-up mode (no argument)
Find and persist any uncommitted edits in the source dir.

1. `git -C ~/.local/share/chezmoi status` and `git diff` to see what's there.
2. If the working tree is clean, say so and exit.
3. Skim the diff. If anything looks accidental — a non-managed file appearing, an obviously half-finished edit, a hardcoded secret in plaintext — pause and ask before continuing.
4. Otherwise, proceed to **Commit**.

## Commit
- Stage explicitly named files. Never `git add -A` or `git add .`.
- Generate a focused commit message in the existing repo style: short imperative subject, body explaining the WHY (not the WHAT) in 1-2 sentences. Skim `git log --oneline -10` if you're unsure of tone.
- Commit with the Co-Authored-By trailer for Claude Code.
- The repo's post-commit hook handles the push.
- Report the new commit SHA and a one-line summary of what landed.
