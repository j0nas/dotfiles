source ~/.zsh/zsh-autosuggestions/zsh-autosuggestions.zsh
source $ZSH/oh-my-zsh.sh
. ~/z.sh

alias dc="docker-compose"
alias x="xclip -selection clipboard"

ZSH_THEME="robbyrussell"
plugins=(git npm)

export ZSH="/home/jonas/.oh-my-zsh"
export SSH_KEY_PATH="~/.ssh/rsa_id"
export NVM_DIR="$HOME/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"  # This loads nvm
[ -s "$NVM_DIR/bash_completion" ] && \. "$NVM_DIR/bash_completion"  # This loads nvm bash_completion
