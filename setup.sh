#!/bin/bash
set -e

echo "==> Setting up dotfiles..."

OS="$(uname -s)"

# --- macOS: enable Touch ID for sudo (one-time, before any other sudo) ---
# Lets brew/installer/chezmoi-driven sudo prompts work in non-TTY contexts
# (the Touch ID prompt is a system-level GUI dialog, not stdin). Without
# this, `brew install --cask <pkg-with-.pkg-artifact>` fails when run from
# any non-interactive shell. /etc/pam.d/sudo_local survives system updates;
# /etc/pam.d/sudo already includes it (Apple-supplied template at
# /etc/pam.d/sudo_local.template documents this exact line).
if [[ "$OS" == "Darwin" ]] && [[ ! -f /etc/pam.d/sudo_local ]]; then
  echo "==> Enabling Touch ID for sudo (one password prompt, then never again)..."
  echo "auth       sufficient     pam_tid.so" | sudo tee /etc/pam.d/sudo_local > /dev/null
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
