#!/bin/bash
set -e

echo "==> Setting up dotfiles..."

OS="$(uname -s)"
IS_WSL=false
if [[ -f /proc/version ]] && grep -qi microsoft /proc/version; then
  IS_WSL=true
fi

# --- macOS: install Homebrew ---
if [[ "$OS" == "Darwin" ]]; then
  if ! command -v brew &> /dev/null; then
    echo "==> Installing Homebrew..."
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
    eval "$(/opt/homebrew/bin/brew shellenv)"
  fi
fi

# --- Linux: install zsh ---
if [[ "$OS" == "Linux" ]]; then
  if ! command -v zsh &> /dev/null; then
    echo "==> Installing zsh (requires sudo)..."
    sudo apt update -qq && sudo apt install -y zsh
  fi
  if [[ "$SHELL" != */zsh ]]; then
    echo "==> Setting zsh as default shell..."
    chsh -s "$(which zsh)"
  fi
fi

# --- WezTerm ---
if [[ "$OS" == "Darwin" ]]; then
  if ! command -v wezterm &> /dev/null; then
    echo "==> Installing WezTerm..."
    brew install --cask wezterm
  fi
elif [[ "$IS_WSL" == true ]]; then
  if ! command -v wezterm.exe &> /dev/null; then
    echo "==> Installing WezTerm on Windows..."
    powershell.exe -NoProfile -Command "winget install --accept-source-agreements --accept-package-agreements wez.wezterm" || \
      echo "NOTE: If winget failed, install WezTerm manually: winget install wez.wezterm"
  fi
fi

# --- chezmoi ---
if ! command -v chezmoi &> /dev/null; then
  echo "==> Installing chezmoi..."
  sh -c "$(curl -fsLS get.chezmoi.io)" -- -b ~/.local/bin
fi
export PATH="$HOME/.local/bin:$PATH"

echo "==> Applying dotfiles..."
chezmoi init --apply j0nas

echo ""
echo "==> All done! Open WezTerm and enjoy your new shell."
