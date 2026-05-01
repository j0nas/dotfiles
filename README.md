# dotfiles

Personal config for macOS, Windows (WSL), and Linux.

## Setup on a new machine

Windows: first install WSL from PowerShell with `wsl --install -d Ubuntu`.

Then, on any platform:

```sh
curl -fsLS https://raw.githubusercontent.com/j0nas/dotfiles/master/setup.sh | bash
```

## How it's wired

**[chezmoi](https://www.chezmoi.io/)** owns dotfiles. This repo is the source of truth, cloned to `~/.local/share/chezmoi`. Files prefixed `dot_` map to `~/.*` after templates render with per-machine data; `chezmoi apply` propagates changes.

**[mise](https://mise.jdx.dev/)** owns CLI tool versions (node, gh, starship, zoxide, fzf, claude). Tool list: `dot_config/mise/config.toml`.

**[antidote](https://github.com/mattmc3/antidote)** is the zsh plugin manager; plugins listed in `dot_zsh_plugins.txt`.

**GUI app package lists** live in `.chezmoidata.yaml`, ranged over by `run_once_install.sh.tmpl` to install via brew (macOS), winget (Windows), or flatpak (Linux). Add a package: edit the YAML, run `chezmoi apply`.

## Obsidian + iCloud notes

The vault lives in iCloud. Each platform reaches it differently:

- **macOS** — iCloud Drive is native. Vault path: `~/Library/Mobile Documents/iCloud~md~obsidian/Documents/Jonas' Vault`.
- **Windows (WSL)** — `run_once_install.sh.tmpl` runs `winget install Apple.iCloud`. Sign in and tick *iCloud Drive*; the vault syncs to `C:\Users\<user>\iCloudDrive\iCloud~md~obsidian\Jonas' Vault`. Re-run `chezmoi apply` once sync completes so the plugin script can find it.
- **Linux** — no official iCloud client. Use Obsidian Sync, Syncthing, or git for cross-device sync.

`run_onchange_setup-obsidian-tasks.sh.tmpl` drops the pinned [Tasks](https://github.com/obsidian-tasks-group/obsidian-tasks) plugin into the vault and enables it. Appends rather than replaces, so existing plugins survive. Bump `PLUGIN_VERSION` to upgrade.
