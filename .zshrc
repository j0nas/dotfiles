export ZSH="/home/jonas/.oh-my-zsh"
export ZSH_THEME="robbyrussell"
export SSH_KEY_PATH="~/.ssh/rsa_id"
export TERM=xterm-256color
export NVM_DIR="$HOME/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"  # This loads nvm
[ -s "$NVM_DIR/bash_completion" ] && \. "$NVM_DIR/bash_completion"  # This loads nvm bash_completion

alias dc="docker-compose"
alias x="xclip -selection clipboard"

plugins=(git npm)

. ~/z.sh
. $ZSH/oh-my-zsh.sh
. ~/.zsh/zsh-autosuggestions/zsh-autosuggestions.zsh
. ~/.zsh/alias-tips/alias-tips.plugin.zsh
