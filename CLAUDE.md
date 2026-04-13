# Dotfiles repo managed by chezmoi

## Key rules

- **Three OS targets**: macOS, Linux, and WSL. WSL is NOT the same as Linux — it has Windows-specific quirks (ConPTY, `/mnt/c`, symlinks don't cross OS boundary).
- **Don't hardcode usernames**. Use `{{ .chezmoi.username }}` in templates or `${USER}` in shell scripts.
- **chezmoi naming**: dotfiles use `dot_` prefix, templates end in `.tmpl`. Files in `.chezmoiignore` are repo-only (not applied).
- **All aliases go in** `dot_config/zsh/aliases.zsh.tmpl` — nowhere else.
- **Shared data** lives in `.chezmoi.toml.tmpl` (`font`, `name`, `email`). Use template variables instead of hardcoding.
- **WezTerm config** (`dot_wezterm.lua.tmpl`) is a chezmoi template. Use `wezterm.target_triple:find("windows")` for Windows-specific Lua logic. On WSL, this file is copied (not symlinked) to the Windows home by `run_onchange_sync-wezterm.sh.tmpl`.
- **VS Code settings** (`dot_config/Code/User/settings.json.tmpl`) — on WSL, copied to Windows side by `run_onchange_sync-vscode.sh.tmpl`.
- **mise manages CLI tools**, not GUI apps. WezTerm is installed via brew/winget in `setup.sh`.
- **`run_once_install.sh.tmpl`** handles userspace bootstrapping (mise, antidote, fonts). System-level installs (zsh, WezTerm) go in `setup.sh`.
- After editing wezterm config, run `chezmoi apply` to sync the Windows copy.
