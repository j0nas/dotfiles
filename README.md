# dotfiles

Managed with [chezmoi](https://www.chezmoi.io/). Tools managed with [mise](https://mise.jdx.dev/).

## Setup on a new machine

```sh
sh -c "$(curl -fsLS get.chezmoi.io)" -- init --apply j0nas
```

This single command installs chezmoi, clones this repo, applies configs, and runs the bootstrap script which installs mise, all tools, and the zsh plugin manager.

### Manual steps after setup

**Linux/WSL only:**
- Install zsh: `sudo apt install -y zsh && chsh -s $(which zsh)`

**All platforms:**
- In Warp (or your terminal), set the font to **JetBrains Mono Nerd Font**

## What's included

**Configs:** zsh, tmux, nano, starship (prompt)

**Tools (via mise):** starship, zoxide, fzf, tmux, gh

**Zsh plugins (via antidote):** zsh-autosuggestions, zsh-syntax-highlighting
