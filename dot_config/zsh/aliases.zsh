alias g='git'                                      # sc: g | git | Git
alias ga='git add'                                 # sc: ga | git add | Git
alias gaa='git add --all'                          # sc: gaa | git add --all | Git
alias gb='git branch'                              # sc: gb | git branch | Git
alias gcb='git checkout -b'                        # sc: gcb | git checkout -b | Git
alias gco='git checkout'                           # sc: gco | git checkout | Git
alias gc='git commit --verbose'                    # sc: gc | git commit --verbose | Git
alias gcam='git commit --all --message'            # sc: gcam | git commit --all --message | Git
alias gc!='git commit --verbose --amend'           # sc: gc! | git commit --verbose --amend | Git
alias gd='git diff'                                # sc: gd | git diff | Git
alias gl='git pull'                                # sc: gl | git pull | Git
alias gp='git push'                                # sc: gp | git push | Git
alias gst='git status'                             # sc: gst | git status | Git

# docker
alias dc='docker compose'                          # sc: dc | docker compose | Docker

# clipboard
# sc: x | Copy stdin to the clipboard | Clipboard
alias x='pbcopy'

# navigation
mcd() { mkdir -p -- "$1" && cd -- "$1"; }          # sc: mcd <dir> | mkdir -p then cd into it | Navigation
# Each extra dot climbs one more level, as in oh-my-zsh. Global aliases expand as
# a whole word anywhere on the line: typed alone, AUTO_CD (dot_zshrc) cds there,
# and `ls ....` or `cp f ...` work too. Quote it ('...') to pass literal dots.
# sc: ... / .... / ..... | cd up 2, 3, 4 levels (also works as an argument) | Navigation
alias -g ...='../..'
alias -g ....='../../..'
alias -g .....='../../../..'
alias -g ......='../../../../..'

# mtg proxies. The project (github.com/j0nas/mtg-proxy, private) lives at
# ~/Desktop/projects/code/mtg-proxy on the Mac; MTG_PROXY_DIR overrides. The
# whole flow runs there and prints through the PC's Epson queues over SSH, so
# the PC keeps no clone of its own (two clones drifted apart more than once).
# A function, not an alias, so a machine without the clone is told what to do
# instead of "no such file".
_mtg_proxy_dir() {
  local d
  for d in "${MTG_PROXY_DIR:-}" "$HOME/Desktop/projects/code/mtg-proxy"; do
    [[ -n "$d" && -d "$d" ]] && { echo "$d"; return 0; }
  done
  echo "$1: mtg-proxy clone not found — run: gh repo clone j0nas/mtg-proxy ~/Desktop/projects/code/mtg-proxy" >&2
  return 1
}
# A printed paper deck box's cut, the formeriet workbench's own command (a subshell, so the
# caller stays where it was): cut-box <deck> [--proof] [--passes N] [--dry-run]
# sc: cut-box <deck> | Cut a printed deck box on the Cameo (formeriet workbench) | Tools
cut-box() {
  local dir="${FORMERIET_DIR:-$HOME/Desktop/projects/code/formeriet}/apps/paper-deck-box"
  [[ -d "$dir" ]] || { echo "cut-box: no workbench at $dir — run: gh repo clone j0nas/formeriet ~/Desktop/projects/code/formeriet" >&2; return 1; }
  (cd "$dir" && pnpm -s cut "$@")
}

# The one proxy command: bare, it is a wizard through making a deck's sheets,
# printing them on the PC and cutting them on the Cameo (plus redo, backlog,
# notes). `mtg-proxy make|print|cut|…` run the same steps directly.
# sc: mtg-proxy [cmd] | MTG proxies: make, print on the PC, cut on the Cameo | Tools
mtg-proxy() {
  local dir; dir=$(_mtg_proxy_dir mtg-proxy) || return 1
  "$dir/mtg-proxy.sh" "$@"
}

# tools
alias c='claude'                                   # sc: c | claude | Tools
alias cr='claude --resume'                         # sc: cr | claude --resume | Tools
# ~/.claude/settings.json (chezmoi-managed) leaves `model` unset, so plain `c`
# runs the account default; `cf`, `co` and `cs` are the session overrides. --model
# applies only to the session it starts, while /model also writes `model` into
# settings.json, so a mid-session switch reads back as chezmoi drift. Effort comes
# from modelSettings in Claude-settings.json.
alias cf='claude --model fable'                    # sc: cf | claude on Fable | Tools
alias co='claude --model opus'                     # sc: co | claude on Opus | Tools
alias cs='claude --model sonnet'                   # sc: cs | claude on Sonnet | Tools
alias md='glow -p'                                 # sc: md | Read a markdown file, paged (glow) | Tools
alias keys='glow -p "$HOME/.local/share/chezmoi/SHORTCUTS.md"'  # sc: keys | Open this shortcuts cheat sheet | Tools
alias board='project-board'                        # sc: board | Kanban board of every project in ~/Desktop/projects (opens in the browser) | Tools

# sc: kill-port <port> | Kill whatever process is listening on a port | Tools
kill-port() {
  local port="$1"
  [[ -n "$port" ]] || { echo "usage: kill-port <port>" >&2; return 1; }
  local pids
  pids=$(lsof -ti ":$port")
  [[ -n "$pids" ]] || { echo "kill-port: nothing listening on port $port" >&2; return 1; }
  echo "kill-port: killing PID(s) ${pids//$'\n'/ } on port $port" >&2
  echo "$pids" | xargs kill
}

# The agents' own Brave: a separate profile (~/.brave-agent) with the Chrome DevTools
# Protocol always on :9222, for agent-browser and CDP scripts. It runs beside the everyday
# Brave (`open -n` starts a second instance), which never gets the port: on the main
# profile it would hand every logged-in session to any local process, and an automation
# run that wedges the browser would take those tabs down too. The profile holds only the
# logins agents need, signed in by hand once. Already listening: nothing to do.
# sc: brave-agent | Launch the agents' Brave (own profile, CDP on :9222) | Tools
brave-agent() {
  if curl -s -m 1 127.0.0.1:9222/json/version 2>/dev/null | grep -q '"Browser"'; then
    echo "brave-agent: already running on :9222" >&2
    return 0
  fi
  # An answer that isn't DevTools' JSON is the everyday Brave with brave://inspect's
  # remote-debugging toggle on: it holds 127.0.0.1:9222 and asks before each connection,
  # and the agents' Brave would get only [::1]:9222.
  if lsof -nP -iTCP@127.0.0.1:9222 -sTCP:LISTEN >/dev/null 2>&1; then
    echo "brave-agent: something else holds 127.0.0.1:9222, likely your own Brave's remote-debugging toggle; untick it at brave://inspect/#remote-debugging" >&2
    return 1
  fi
  open -na "Brave Browser" --args --user-data-dir="$HOME/.brave-agent" \
    --remote-debugging-port=9222 --no-first-run --no-default-browser-check
}

# Scheduled shutdown: BSD `shutdown -h +m`, which needs root.
# sc: shutdown-in <minutes> | Schedule a shutdown that many minutes from now | Tools
shutdown-in() {
  local m="$1"
  [[ "$m" =~ ^[0-9]+$ ]] || { echo "usage: shutdown-in <minutes>" >&2; return 1; }
  sudo shutdown -h "+$m" || return
  echo "shutdown-in: shutdown scheduled for $(date -v "+${m}M" +%H:%M)" >&2
}
# sc: shutdown-cancel | Cancel a pending shutdown-in | Tools
shutdown-cancel() { sudo killall shutdown; }
