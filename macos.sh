/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
echo '# Set PATH, MANPATH, etc., for Homebrew.' >> /Users/jonasjensen/.zprofile
echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> /Users/jonasjensen/.zprofile
eval "$(/opt/homebrew/bin/brew shellenv)"

source ~/.zshrc
git clone https://github.com/zsh-users/zsh-autosuggestions ${ZSH_CUSTOM:-~/.oh-my-zsh/custom}/plugins/zsh-autosuggestions

brew install --cask microsoft-outlook
brew install slack
brew install zsh
brew install tmux
brew install z
brew install gh
brew install docker-compose
brew install --cask docker
brew install --cask alfred
brew install --cask google-chrome
brew install --cask microsoft-teams
brew install --cask visual-studio-code
brew install --cask signal

defaults write -g InitialKeyRepeat -int 10 # normal minimum is 15 (225 ms)
defaults write -g KeyRepeat -int 1 # normal minimum is 2 (30 ms)

