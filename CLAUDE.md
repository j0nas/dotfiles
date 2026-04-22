# Dotfiles repo managed by chezmoi

## Key rules

- **Setup is idempotent.** `chezmoi apply`, `setup.sh`, and the `run_once_*` scripts must be safe to re-run: no duplicate appends, no clobbered user state, no crashes on existing installs. See the package-manager section below for specifics.
- **Three OS targets**: macOS, Linux, and WSL. WSL is NOT the same as Linux — it has Windows-specific quirks (ConPTY, `/mnt/c`, symlinks don't cross OS boundary).
- **Don't hardcode usernames**. Use `{{ .chezmoi.username }}` in templates or `${USER}` in shell scripts.
- **chezmoi naming**: dotfiles use `dot_` prefix, templates end in `.tmpl`. Files in `.chezmoiignore` are repo-only (not applied).
- **All aliases go in** `dot_config/zsh/aliases.zsh.tmpl` — nowhere else.
- **Shared data** lives in `.chezmoi.toml.tmpl` (`font`, `name`, `email`). Use template variables instead of hardcoding.
- **WezTerm config** (`dot_wezterm.lua.tmpl`) is a chezmoi template. Use `wezterm.target_triple:find("windows")` for Windows-specific Lua logic. On WSL, this file is copied (not symlinked) to the Windows home by `run_onchange_sync-wezterm.sh.tmpl`.
- **VS Code settings** (`dot_config/Code/User/settings.json.tmpl`) — on WSL, copied to Windows side by `run_onchange_sync-vscode.sh.tmpl`.
- **mise manages CLI tools**, not GUI apps. GUI apps (WezTerm, Obsidian, Signal, …) go in `run_once_install.sh.tmpl` arrays.
- **`run_once_install.sh.tmpl`** handles userspace bootstrapping (mise, antidote, fonts, GUI apps). System-level installs (zsh) go in `setup.sh`.
- After editing wezterm config, run `chezmoi apply` to sync the Windows copy.

## Package-manager idempotency

GUI apps go in per-platform arrays (`CASKS`, `WINGET_PKGS`, `FLATPAKS`) in
`run_once_install.sh.tmpl`. Trust the package manager — **never** add
filesystem existence guards (`[[ -d /Applications/X.app ]]`, `command -v x.exe`).
Those paths vary by install location (`Program Files` vs `%LocalAppData%\Programs`
vs `WindowsApps`) and go stale.

- `brew install [--cask] X` — exit 0 + warning if already installed. ✓
- `flatpak install -y flathub X` — exit 0 + "already installed, skipping". ✓
- `winget install --id X` — **non-zero** exit (`0x8A150061`) if already
  installed. Must wrap in `|| true` or `set -e` aborts the bootstrap.

Adding a new GUI app = append one id to each platform's array. No new
conditionals, no path checks.
