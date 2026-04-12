# dotfiles

Managed with [chezmoi](https://www.chezmoi.io/). Tools managed with [mise](https://mise.jdx.dev/).

## Setup on a new machine

```sh
sh -c "$(curl -fsLS get.chezmoi.io)" -- init --apply j0nas
curl https://mise.run | sh
mise install
```

## What's included

**Configs:** zsh, tmux, nano, starship (prompt)

**Tools (via mise):** starship, zoxide, fzf, tmux, gh

**Zsh plugins (via antidote):** zsh-autosuggestions, zsh-syntax-highlighting
