---
name: dotfiles
description: Make an environment change reproducible — route the change to the right file in the chezmoi dotfiles repo, apply the repo's conventions, and commit + push.
argument-hint: "[change you want to make, or empty to catch up on dirty state]"
---

You are working in the user's chezmoi dotfiles repo at `~/.local/share/chezmoi`. Read `~/.local/share/chezmoi/AGENTS.md` first if you haven't already this session — it documents the conventions you must follow.

## Step 0 — Sync first (always)

Before reading or editing anything, sync the source dir with its remote so your commit doesn't diverge: `git -C ~/.local/share/chezmoi pull --rebase --autostash`. chezmoi's autoPush means another `chezmoi edit` (here or on another machine) can leave the remote ahead; pulling first avoids the rebase-conflict dance at push time. `--autostash` keeps any in-progress working-tree edits (catch-up mode) safe. If the rebase surfaces a conflict, resolve it before starting the requested change.

## Two modes

### Forward mode (argument provided)
The argument describes a change the user wants persisted across machines.

1. **Route** the change to the right file/directory. Common patterns:
   - shell alias → `dot_config/zsh/aliases.zsh.tmpl`
   - Homebrew brew or cask, flatpak, winget package, or VS Code extension → `.chezmoidata.yaml`
   - new personal skill → `skills/<name>/SKILL.md` + `dot_claude/skills/symlink_<name>.tmpl` (contents `{{ .chezmoi.sourceDir }}/skills/<name>`)
   - new slash command → `commands/<name>.md` + `dot_claude/commands/symlink_<name>.md.tmpl` (contents `{{ .chezmoi.sourceDir }}/commands/<name>.md`)
   - secret → `chezmoi edit-encrypted dot_claude/secrets/<file>.json`
   - macOS default → `.chezmoiscripts/run_onchange_macos-defaults.sh.tmpl`
   - one-time setup task → `.chezmoiscripts/run_once_<name>.sh.tmpl` (alphabetical sort matters)
   - re-runs on content change → `.chezmoiscripts/run_onchange_<name>.sh.tmpl`
2. **Apply repo rules** from AGENTS.md: idempotency, no hardcoded usernames, OS gating with `{{ if eq .chezmoi.os "darwin" -}}`, shared data via `.chezmoi.toml.tmpl` / `.chezmoidata.yaml`.
3. **Make the edit** in the chezmoi source dir.
4. **Apply** with `chezmoi apply` and read the output for errors. This both validates the template and lands the change locally.
5. Proceed to **Commit**.

### Catch-up mode (no argument)
Find and persist any uncommitted edits in the source dir.

1. `git -C ~/.local/share/chezmoi status` and `git diff` to see what's there.
2. If the working tree is clean, say so and exit.
3. Skim the diff. If anything looks accidental — a non-managed file appearing, an obviously half-finished edit, a hardcoded secret in plaintext — pause and ask before continuing.
4. `chezmoi apply` to land any pending source-dir changes locally before committing.
5. Otherwise, proceed to **Commit**.

## Commit
- Stage explicitly named files. Never `git add -A` or `git add .`.
- Generate a focused commit message in the existing repo style: short imperative subject, body explaining the WHY (not the WHAT) in 1-2 sentences. Skim `git log --oneline -10` if you're unsure of tone.
- Do **not** add a `Co-Authored-By: Claude` or `🤖 Generated with Claude Code` trailer — attribution is intentionally disabled globally (`attribution.commit: ""` in `dot_claude/settings.json`). Keep the message clean.
- Push explicitly with `git -C ~/.local/share/chezmoi push` and confirm it succeeded. Don't rely on chezmoi's autoCommit/autoPush — those only fire on `chezmoi edit`-driven commits, not the direct `git commit` above.

## Post-commit apply (always)
After every successful push, run `chezmoi apply` to converge the local machine to the committed state. Since apply is idempotent this is always safe, and it catches any `run_onchange_` scripts whose hash changed (e.g. a new tool added to mise config, a new package added to `.chezmoidata.yaml`). Read the output and report anything non-trivial that ran.

- Report the new commit SHA and a one-line summary of what landed.
