# Dotfiles repo managed by chezmoi

## Key rules

- **Three OS targets**: macOS, Linux, and WSL. WSL is NOT the same as Linux — it has Windows-specific quirks (ConPTY, `/mnt/c`, symlinks don't cross OS boundary).
- **Don't hardcode usernames**. Use `{{ .chezmoi.username }}` in templates or `${USER}` in shell scripts.
- **chezmoi naming**: dotfiles use `dot_` prefix, templates end in `.tmpl`. Files in `.chezmoiignore` are repo-only (not applied).
- **All aliases go in** `dot_config/zsh/aliases.zsh.tmpl` — nowhere else.
- **WezTerm config** (`dot_wezterm.lua`) is Lua, not a chezmoi template. Use `wezterm.target_triple:find("windows")` for Windows-specific logic. On WSL, this file is copied (not symlinked) to the Windows home by `run_onchange_sync-wezterm.sh.tmpl`.
- **mise manages CLI tools**, not GUI apps. WezTerm is installed via brew/winget in `setup.sh`.
- **`run_once_install.sh.tmpl`** handles userspace bootstrapping (mise, antidote, fonts). System-level installs (zsh, WezTerm) go in `setup.sh`.
- After editing wezterm config, run `chezmoi apply` to sync the Windows copy.
