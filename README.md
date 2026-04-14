# dotfiles

Managed with [chezmoi](https://www.chezmoi.io/). Tools managed with [mise](https://mise.jdx.dev/).

## Setup on a new machine

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/j0nas/dotfiles/master/setup.sh)
```

One command. Installs everything: zsh, WezTerm, chezmoi, mise, CLI tools, Nerd Font, and zsh plugins.

## What's included

**Terminal:** WezTerm (Catppuccin Mocha theme, JetBrains Mono Nerd Font)

**Configs:** zsh, git, nano, starship (prompt), Claude Code, VS Code

**Tools (via mise):** starship, zoxide, fzf, gh, claude, node (LTS)

**VS Code extensions:** Claude Code, Tailwind CSS, Scratchpads, Catppuccin

**Zsh plugins (via antidote):** zsh-autosuggestions, zsh-syntax-highlighting

**Obsidian** (vault: `Jonas' Vault`, synced via iCloud, with the [Tasks](https://github.com/obsidian-tasks-group/obsidian-tasks) plugin for GTD workflows)

## Obsidian + iCloud notes

The vault lives in iCloud. Each platform reaches it differently:

- **macOS** — iCloud Drive is native. Vault path:
  `~/Library/Mobile Documents/iCloud~md~obsidian/Documents/Jonas' Vault`.
- **Windows (WSL)** — `run_once_install.sh.tmpl` runs `winget install Apple.iCloud`.
  Sign in to iCloud for Windows and tick *iCloud Drive*; the vault syncs to
  `C:\Users\<user>\iCloudDrive\iCloud~md~obsidian\Jonas' Vault`. Re-run
  `chezmoi apply` once the sync completes so the plugin script can find it.
- **Linux** — no official iCloud client. Obsidian installs via flatpak, but
  you'll need Obsidian Sync, Syncthing, or git for cross-device sync.

`run_onchange_setup-obsidian-tasks.sh.tmpl` drops the pinned Tasks release into
`<vault>/.obsidian/plugins/obsidian-tasks-plugin/` and enables it in
`community-plugins.json`. It appends rather than replaces, so existing plugins
in the vault are preserved and iCloud propagates the change to other devices.
Bump `PLUGIN_VERSION` in that script to upgrade.
