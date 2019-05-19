#! /usr/bin/env bash

# Check if the script is running as root
if [ "$EUID" -ne 0 ]; then
  >&2 echo "Please run this script as root!"
  exit 2
fi

# Packages & utilities
add-apt-repository universe
apt -y update
apt -y full-upgrade
apt -y install git zsh curl xclip gnome-tweak-tool

apt-get update
apt-get upgrade
apt-get install powerline fonts-powerline wget

# Apps
snap install --classic slack webstorm
snap install spotify

wget https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
dpkg -i google-chrome-stable_current_amd64.deb

# Copy everything to home folder
cp ./* ~

# Install oh-my-zsh & zsh-autosuggestions
sh -c "$(curl -fsSL https://raw.githubusercontent.com/robbyrussell/oh-my-zsh/master/tools/install.sh)"
git clone https://github.com/zsh-users/zsh-autosuggestions ~/.zsh/zsh-autosuggestions

# Install Node Version Manager, Node.js LTS & tldr package
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.34.0/install.sh | bash
nvm install --lts
npm i -g tldr

# Install Docker & Compose
curl -fsSL https://get.docker.com -o- | bash
usermod -aG docker $USER
curl -L "https://github.com/docker/compose/releases/download/1.24.0/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
chmod +x /usr/local/bin/docker-compose

gnome-tweaks
