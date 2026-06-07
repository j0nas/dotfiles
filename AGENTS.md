# Dotfiles repo managed by chezmoi

## Key rules

- **Setup is idempotent.** `chezmoi apply`, `setup.sh`, and the `run_once_*` / `run_onchange_*` scripts must be safe to re-run: no duplicate appends, no clobbered user state, no crashes on existing installs.
- **Three OS targets**: macOS, Linux, and WSL. WSL is NOT the same as Linux — it has Windows-specific quirks (ConPTY, `/mnt/c`, symlinks don't cross OS boundary).
- **Don't hardcode usernames**. Use `{{ .chezmoi.username }}` in templates or `${USER}` in shell scripts.
- **chezmoi naming**: dotfiles use `dot_` prefix, templates end in `.tmpl`. Files in `.chezmoiignore` are repo-only (not applied).
- **Script execution order is alphabetical by *stripped* name** (without the `run_*` prefix). `run_once_bootstrap.sh.tmpl` must sort before `run_onchange_install-*.sh.tmpl` so the package manager exists before package install runs.
- **All aliases go in** `dot_config/zsh/aliases.zsh.tmpl` — nowhere else.
- **Shared data** lives in `.chezmoi.toml.tmpl` (`font`, `name`, `email`) and `.chezmoidata.yaml` (package + extension lists). Use template variables instead of hardcoding.
- **WezTerm config** (`dot_wezterm.lua.tmpl`) is a chezmoi template. Use `wezterm.target_triple:find("windows")` for Windows-specific Lua logic. On WSL, this file is copied (not symlinked) to the Windows home by `run_onchange_sync-wezterm.sh.tmpl`.
- **VS Code settings** — the real content is the repo-only `Code-settings.json` (a static file, not a template; ignored in `.chezmoiignore`). Each OS gets a symlink to it via `symlink_settings.json.tmpl`: macOS at `private_Library/private_Application Support/Code/User/`, Linux/WSL at `dot_config/Code/User/`. `.chezmoiignore` gates which symlink applies per OS. The symlink means VS Code's own writes (Settings UI) flow straight back into the repo (auto-committed). On WSL, the settings are also copied to the Windows side by `run_onchange_sync-vscode.sh.tmpl`. Note: symlinked = not templated, so the font is hardcoded here rather than using `{{ "{{ .font }}" }}`.
- **SwiftBar** (menu-bar host for the AeroSpace workspace-label plugin) is configured headlessly — no GUI clicks. `run_onchange_after_setup-swiftbar.sh.tmpl` (darwin-gated, `after_` so it runs once the files below exist) writes the plugin folder to the `PluginDirectory` defaults key (SwiftBar stores it as a plain string and isn't sandboxed, so no security-scoped bookmark) and loads the login agent. Launch-at-login is a tracked LaunchAgent at `private_Library/LaunchAgents/com.ameba.swiftbar.autostart.plist` (`open -a SwiftBar`, `RunAtLoad`) because SwiftBar's own toggle is GUI-only `SMAppService`; the existing `.chezmoiignore` `Library` rule gates it to darwin. The plugin itself is `dot_config/swiftbar/plugins/executable_aerospace.10s.sh`. The instant menu-bar update relies on the `exec-on-workspace-change` hook in `dot_aerospace.toml.tmpl`; AeroSpace only reads its config at launch, so `run_onchange_after_reload-aerospace.sh.tmpl` (keyed on the config hash) runs `aerospace reload-config` on every apply that touches the config — otherwise a newly-added hook stays dormant in the running instance and the label only updates on the plugin's 10s poll.
- **mise manages CLI tools** (declared in `dot_config/mise/config.toml`), converged on every apply by `run_onchange_install-mise-tools.sh.tmpl` (keyed on that config's hash). GUI apps + VS Code extensions live in `.chezmoidata.yaml`, consumed by `run_onchange_install-packages.sh.tmpl` / `run_onchange_install-vscode-extensions.sh.tmpl`.
- **Node is delegated to fnm**, not mise. fnm is a mise-installed tool that owns Node versions and auto-switches per `.node-version`/`.nvmrc` (shell init in `dot_zshrc.tmpl`, after mise activation). The global default version is `node_default` in `.chezmoidata.yaml`, applied by the mise-tools converge script. Use `fnm use <v>` for on-the-fly switches.
- **`run_onchange_` vs `run_once_`**: anything tracking *desired state that can change* (tool lists, package lists, default versions) belongs in a `run_onchange_` script keyed on that state — embed `{{ include "file" | sha256sum }}` or reference the data var — so `chezmoi apply` re-converges when the declaration changes. Reserve `run_once_` for genuinely one-time bootstrap with no state to track: installing the package managers themselves, cloning antidote, expensive one-shot downloads. Mutable desired-state in `run_once_` is a bug — it won't re-run when you change the declaration.
- **`run_once_bootstrap.sh.tmpl`** handles userspace bootstrap (Homebrew, mise, antidote, agent-browser, Claude MCP, lightpanda, Linux font). System-level installs (zsh) go in `setup.sh`.
- After editing wezterm config, run `chezmoi apply` to sync the Windows copy.
- **Config validation** lives in one script, `.githooks/validate` (the single source of truth), run by both the pre-commit hook and CI. It: renders every `*.tmpl` (`chezmoi execute-template --source` — portable to CI), checks every JSON is well-formed (`jq`), validates files with a published schema against the **pinned** vendored copies in `.githooks/schemas/` (`check-jsonschema` for JSON, `taplo` for TOML), and runs `shellcheck --severity=warning` on every shell script (templates rendered first; reads the shebang from stdin). All tools are mise-managed. Notes: `opencode.json` and VS Code settings are syntax-only (no usable static schema); `shellcheck` threshold is `warning` (info-level SC2016/SC2086 are noisy/often-intentional); WSL-gated scripts render empty off-WSL and are skipped. `git commit --no-verify` bypasses.
- **Hook install**: `run_onchange_install-git-hooks.sh.tmpl` sets `core.hooksPath` to the tracked `.githooks/` (pre-commit = validate, post-commit = auto-push). `.githooks/`, `.github/`, `.taplo.toml` are repo-only (dotted source entries are auto-ignored by chezmoi). To refresh a schema, re-download into `.githooks/schemas/` (see its README).
- **CI** (`.github/workflows/validate.yml`): runs `.githooks/validate` on push/PR across a macOS + Linux matrix, so each platform's rendered templates get linted (the local hook only sees the committer's OS). Tools via `jdx/mise-action` + `mise use -g`; `chezmoi init --apply=false` first so template data resolves.

## Claude Code skills, commands, secrets

- **Personal skills** live as real directories at `skills/<name>/` (repo-only — see `.chezmoiignore`). Each is surfaced into `~/.claude/skills/<name>` via a per-skill symlink file at `dot_claude/skills/symlink_<name>.tmpl` whose contents are `{{ .chezmoi.sourceDir }}/skills/<name>`.
- **Slash commands** follow the same shape: real `commands/<name>.md`, symlinked from `dot_claude/commands/symlink_<name>.md.tmpl`.
- **Editing a skill or command** via the `~/.claude/...` path edits the source file directly (it's a symlink). After editing, invoke `/dotfiles` to commit + push — chezmoi's autoCommit only fires on `chezmoi edit`, not on direct edits to source.
- **Third-party skills** installed by `pnpm dlx skills add` live at `~/.agents/skills/<name>` and surface into `~/.claude/skills/` as symlinks managed by the skills CLI — not by chezmoi. Coexist fine.
- **Secrets** are age-encrypted, decrypt via your SSH key. Vault files use the `encrypted_private_*` prefix (private = mode 0600 on apply). Recipients (machines authorized to decrypt) live in `.age-recipients` at the repo root — public keys, safe to commit. Edit a vault with `chezmoi edit-encrypted dot_claude/secrets/<file>.json`. `age` is installed via the `brews:` list in `.chezmoidata.yaml`.
- **Install-intercept hook** (`dot_claude/hooks/executable_intercept-installs.sh`, wired via `PreToolUse`/`Bash` in `dot_claude/settings.json`): denies Claude's *direct* global/system install commands (`brew install`, `mise use`, `npm/pnpm -g`, `pipx/cargo/flatpak install`, …) and points to `/dotfiles` so they get persisted. It's silent (exit 0, no output) for everything else. Escape hatch: append `# one-off` to the command for an explicit throwaway. Installs nested inside `chezmoi apply` don't trigger it (they run in chezmoi's process, not a Bash tool call), so /dotfiles never blocks itself.

## Package management

GUI apps and VS Code extensions are declared in `.chezmoidata.yaml` and consumed by `run_onchange_*` scripts that re-run when the data changes.

- **macOS** (`brew bundle` with `cleanup --force`) — strict declarative state. Removing a cask from the YAML uninstalls it.
- **VS Code extensions** — same: extensions not in YAML get uninstalled.
- **winget** (Windows) — install-only. Removing from YAML does NOT uninstall.
- **flatpak** (Linux) — install-only. Removing from YAML does NOT uninstall.

Trust the package manager — **never** add filesystem existence guards (`[[ -d /Applications/X.app ]]`, `command -v x.exe`). Install paths vary (`Program Files` vs `%LocalAppData%\Programs` vs `WindowsApps`) and go stale.

- `brew bundle` — exit 0, idempotent. ✓
- `flatpak install -y flathub X` — exit 0 + "already installed, skipping". ✓
- `winget install --id X` — **non-zero** exit (`0x8A150061`) if already installed. Must wrap in `|| true`.

Adding a new GUI app = one line in `.chezmoidata.yaml` per platform.
