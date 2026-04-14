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

iCloud sync is set up differently on each platform:

- **macOS** — iCloud Drive is native. The Obsidian app container lives at
  `~/Library/Mobile Documents/iCloud~md~obsidian/Documents/Jonas' Vault`.
- **Windows (WSL)** — `run_once_install.sh.tmpl` runs `winget install Apple.iCloud`.
  After first install you must sign in to iCloud for Windows and tick
  *iCloud Drive*. Vault path: `C:\Users\<user>\iCloudDrive\iCloud~md~obsidian\Jonas' Vault`.
- **Linux** — there is no official iCloud client. Obsidian is installed via
  flatpak, but you'll need Obsidian Sync, Syncthing, or git for cross-device
  sync on Linux.

**Important:** create the vault on an Apple device first (e.g. Obsidian iOS →
*Create new vault* → *Store in iCloud*). Creating it on Windows first breaks
iOS discovery. After the container has synced down, re-run `chezmoi apply` and
`run_onchange_setup-obsidian-tasks.sh.tmpl` will drop the Tasks plugin into
`<vault>/.obsidian/plugins/obsidian-tasks-plugin/` and enable it in
`community-plugins.json`.
