#!/bin/bash
set -e

echo "==> Setting up dotfiles..."

OS="$(uname -s)"

# --- macOS: install Homebrew ---
if [[ "$OS" == "Darwin" ]]; then
  if ! command -v brew &> /dev/null; then
    echo "==> Installing Homebrew..."
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
    eval "$(/opt/homebrew/bin/brew shellenv)"
  fi
fi

# --- Linux/WSL: install zsh (requires sudo, not handled by chezmoi) ---
if [[ "$OS" == "Linux" ]]; then
  if ! command -v zsh &> /dev/null; then
    echo "==> Installing zsh (requires sudo)..."
    sudo apt update -qq && sudo apt install -y zsh
  fi
  if [[ "$SHELL" != */zsh ]]; then
    echo "==> Setting zsh as default shell..."
    chsh -s "$(command -v zsh)"
  fi
fi

# --- chezmoi ---
if ! command -v chezmoi &> /dev/null; then
  echo "==> Installing chezmoi..."
  sh -c "$(curl -fsLS get.chezmoi.io)" -- -b ~/.local/bin
fi
export PATH="$HOME/.local/bin:$PATH"

echo "==> Applying dotfiles..."
# `init --apply` does NOT pull on re-runs if the source already exists, so
# `update` (= git pull + apply) is the idempotent choice once initialized.
if [[ -d "$HOME/.local/share/chezmoi/.git" ]]; then
  chezmoi update
else
  chezmoi init --apply j0nas
fi

echo ""
echo "==> All done! Open WezTerm and enjoy your new shell."
