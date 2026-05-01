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
- **VS Code settings** (`dot_config/Code/User/settings.json.tmpl`) — on WSL, copied to Windows side by `run_onchange_sync-vscode.sh.tmpl`.
- **mise manages CLI tools**; GUI apps + VS Code extensions live in `.chezmoidata.yaml`, consumed by `run_onchange_install-packages.sh.tmpl` / `run_onchange_install-vscode-extensions.sh.tmpl`.
- **`run_once_bootstrap.sh.tmpl`** handles userspace bootstrap (Homebrew, mise, antidote, agent-browser, Claude MCP, lightpanda, Linux font). System-level installs (zsh) go in `setup.sh`.
- After editing wezterm config, run `chezmoi apply` to sync the Windows copy.

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
