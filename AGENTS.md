# Dotfiles repo managed by chezmoi (macOS only)

Each script and config explains itself in its header; this file holds what spans files. Read the header before changing one.

## Rules

- **Idempotent**: `chezmoi apply`, `setup.sh` and every `run_*` script must be safe to re-run: no duplicate appends, no clobbered user state, no crash on an existing install.
- **A first apply on a new Mac must work**, with no SSH key, no login shell and nothing installed but what `setup.sh` put there. Scripts may call `brew` and cask CLIs by name only because `setup.sh` puts Homebrew on PATH. CI runs the whole thing (`.github/workflows/new-mac.yml`).
- **No hardcoded usernames**: `{{ .chezmoi.username }}` / `{{ .chezmoi.homeDir }}` in templates, `$USER` / `$HOME` in scripts.
- **No model IDs**: refer to Claude models by alias (`opus`, `sonnet`, `haiku`, `fable`). `modelSettings` in `Claude-settings.json` accepts aliases too, though its docs only show IDs.
- **This repo is public.** Nothing goes in that shouldn't be read by anyone.
- **Naming**: `dot_` prefix, `.tmpl` only where something is templated. Repo-only files are listed in `.chezmoiignore`, whose patterns match **target** paths (`validate` checks).
- **Shared data**: `.chezmoi.toml.tmpl` (`name`, `email`, machine facts; re-run `chezmoi init` after editing) and `.chezmoidata.yaml` (package and extension lists).
- **All aliases** go in `dot_config/zsh/aliases.zsh`.

## Scripts

- `run_*` scripts live in `.chezmoiscripts/` and run in alphabetical order of their name without the `run_*` prefix, interleaved with the files by target path: they run before `.claude` and `.config` are written. `after_` ones run last. **A script that reads a target file needs `after_`.**
- Desired state that can change (lists, versions, defaults) goes in a `run_onchange_` keyed on it: embed `{{ include "file" | sha256sum }}` or the data var. `run_once_` is only for a one-time step with no state.
- Something that should also self-heal on a cadence adds a week key, `{{ div (now | unixEpoch) 604800 }}`. A bare `run_` makes every apply pay for it.
- A run that skips ("not installed yet") still counts as done. Either fail, or put the condition in the rendered script so it changes when the condition does (see `install-fusion-mcp`, `install-hueforge`).
- An apply can stop at a sudo prompt when a cask upgrade wants one. From an agent, use `chezmoi apply --exclude=scripts` or run it in the background, and leave the rest to a terminal.

## Where things go

- **CLI tools**: mise (`dot_config/mise/config.toml`), preferred over any other route. A setup step after each upgrade is a `postinstall` hook. `minimum_release_age = "7d"` is a supply-chain cooldown; an exact pin bypasses it. Node is mise's too; per-project versions come from `mise.toml`, `.node-version`, `.nvmrc` or `package.json` devEngines.
- **Apps and VS Code extensions**: `.chezmoidata.yaml`. `brew bundle cleanup --force` removes what isn't declared, so a one-off is just `brew install <x>`. Trust the package manager: no existence guards (`[[ -d /Applications/X.app ]]`, `command -v`) around installs.
- **Anything downloaded outside a package manager** gets a pinned version and a sha256. A plain download under `$HOME` goes in `.chezmoiexternal.toml`.
- **The repo's own CLI tools** are Go, the default for a new one: one module in `tools/`, a command per `tools/cmd/<name>/`, standard library where possible. The apply builds them into `~/.local/bin` with `GOTOOLCHAIN=local`; keep `go.mod`'s `go` line a minor version so it can't fetch a toolchain past mise.
- **Files an app rewrites itself**:
  - a `modify_` template that sets only the managed keys over the live file (Claude Code's settings, `obsidian.json`), or
  - a symlink to a repo-only file the app writes into, so its changes land here as a diff (`Code-settings.json`, `Tinycast-settings.json`). `/dotfiles` commits those.
- **Desk roles**: hardware tied to one desk is a self-contained dir gated on a boolean in `.chezmoi.toml.tmpl`, derived from the hash of the Mac's hardware UUID and read as `(and (hasKey . "<flag>") .<flag>)`. Current: `g9_desk` → `dot_config/g9-desk/` (its README is the runbook).
- **Retiring a managed file**: `git rm` the source and add an empty `remove_<target name>` beside it; deleting the source alone leaves the target on every Mac. Drop the tombstone once all have applied. A tool dropped from mise's config needs nothing: the converge prunes it.

## Claude Code

- **Skills** `skills/<name>/`, **commands** `commands/<name>.md` and **rules** `rules/<name>.md` are repo-only; each has a stub at `dot_claude/<kind>/symlink_<name>[.md].tmpl` holding `{{ .chezmoi.sourceDir }}/<path>`. Every rule loads into every session, so keep them short.
- Editing one through `~/.claude/...` edits the source. Then run `/dotfiles` to commit: chezmoi's autoCommit only fires on `chezmoi edit`.
- **Settings**: `Claude-settings.json` holds the managed keys and `dot_claude/modify_settings.json` merges them over the live file. To retire a key, add it to that template's `unset` list. `model` stays unmanaged and effort is rebuilt on every apply, so `/model` and `/effort` picks last until then. `MM .claude/settings.json` in `chezmoi status` is normal.
- **Updates**: `autoUpdatesChannel` is `stable` (about a week old, the counterpart of mise's cooldown). Before moving a Mac off `latest`, set a local `minimumVersion` to the running version, or it rolls back. Never commit that floor. For a model that needs a newer build, raise the local floor and run `claude install <version>`; switching to `latest` in `/config` clears it.
- **Third-party skills** (`skills add`) live in `~/.agents/skills`; the mise converge refreshes them.
- **Secrets and private notes**: age-encrypted, `dot_claude/private_secrets/encrypted_private_*.age` → `~/.claude/secrets/`, decrypted with the SSH key and skipped where the Mac's key isn't in `.age-recipients` (its header has the add, revoke and edit steps). A skill about something private (`/seedbox`, `/stationary`) is a public stub that reads its `*-notes.md`.
- **Session log**: `rules/llm-sessions.md`, `commands/session-end.md` and `tools/cmd/llm-log`. `llm-sessions/` is in the global gitignore (`dot_config/git/ignore`), so a log never reaches a remote.

## Validation

- `.githooks/validate` is the single source of truth, run by the pre-commit hook and CI; its header lists the checks. Negative-test a new check against a deliberately broken copy. `git commit --no-verify` bypasses it, CI doesn't.
- `.githooks/` is the repo's `core.hooksPath`: pre-commit validates, post-commit pushes. Schemas are vendored in `.githooks/schemas/` (README there).
- `SHORTCUTS.md` is generated from `# sc: <trigger> | <description> | <section>` tags beside the bindings, in any file with comments. The pre-commit hook regenerates it; never edit it by hand. A tool's defaults get a hand-written tag near where the tool is enabled.
