# dotfiles

Managed with [chezmoi](https://www.chezmoi.io/). Tools managed with [mise](https://mise.jdx.dev/).

## Setup on a new machine

```sh
sh -c "$(curl -fsLS get.chezmoi.io)" -- init --apply j0nas
```

This single command installs chezmoi, clones this repo, applies configs, and runs the bootstrap script which installs mise, all tools, and the zsh plugin manager.

### Manual steps after setup

**Windows/WSL:**
- Install zsh: `sudo apt install -y zsh && chsh -s $(which zsh)`
- Install WezTerm: `winget install wez.wezterm`

## What's included

**Terminal:** WezTerm (Catppuccin Mocha theme, JetBrains Mono Nerd Font)

**Configs:** zsh, tmux, nano, starship (prompt)

**Tools (via mise):** starship, zoxide, fzf, tmux, gh

**Zsh plugins (via antidote):** zsh-autosuggestions, zsh-syntax-highlighting
