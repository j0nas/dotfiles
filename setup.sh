#!/bin/bash
# Set up a Mac from nothing, or re-apply on one already set up. Run it with
#   /bin/bash -c "$(curl -fsSL <raw url of this file>)"
# and never `curl … | bash`: piped, stdin is this script, so Homebrew's installer
# sees no terminal, goes non-interactive and can't ask for the sudo password.
set -euo pipefail

echo "==> Setting up dotfiles..."

# Touch ID for sudo: the prompt is a system dialog, so sudo also works where
# there is no terminal (an apply from an agent or an app). sudo_local survives
# system updates, and Apple's /etc/pam.d/sudo already includes it.
if [[ ! -f /etc/pam.d/sudo_local ]]; then
  echo "==> Enabling Touch ID for sudo (one password prompt, then never again)..."
  echo "auth       sufficient     pam_tid.so" | sudo tee /etc/pam.d/sudo_local > /dev/null
fi

# Homebrew's installer also installs Apple's command line tools, and with them
# the git that the clone below needs.
if [[ ! -x /opt/homebrew/bin/brew ]]; then
  echo "==> Installing Homebrew..."
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi
# The apply's scripts inherit this PATH. They call brew and what it installs
# (code) by name, and a new Mac has no login shell that set it up yet.
eval "$(/opt/homebrew/bin/brew shellenv)"
export PATH="$HOME/.local/bin:$PATH"

# age has to exist before chezmoi starts: chezmoi picks its age when it launches,
# and the one built in can't decrypt with an SSH key.
command -v age > /dev/null || brew install age

# mise runs chezmoi here and manages it from the first apply on.
if ! command -v mise > /dev/null; then
  echo "==> Installing mise..."
  curl -fsSL https://mise.run | sh
fi
chezmoi() { mise exec chezmoi@latest -- chezmoi "$@"; }

echo "==> Applying dotfiles..."
# `init --apply` doesn't pull when the source already exists; `update` does.
# DOTFILES_REPO: another repo or a local path, for testing (CI uses it).
if [[ -d "$HOME/.local/share/chezmoi/.git" ]]; then
  chezmoi update
else
  chezmoi init --apply "${DOTFILES_REPO:-j0nas}"
fi

echo
echo "==> Done. What's left to do by hand is in the README (New Mac checklist)."
