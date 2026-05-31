#!/usr/bin/env bash
# PreToolUse(Bash) hook: nudge global/system installs toward the `/dotfiles`
# workflow so they get persisted in chezmoi instead of becoming untracked
# machine state. Only fires on Claude's *direct* Bash tool calls — installs that
# run *inside* `chezmoi apply` (the /dotfiles path) are nested in chezmoi's own
# process and never reach this hook, so the workflow can't block itself.
#
# Escape hatch: append `# one-off` to the command to bypass (genuine throwaways
# the user has explicitly asked not to check in).
set -u

# Fail open: if we can't parse the command, never get in the way.
command -v jq >/dev/null 2>&1 || exit 0
cmd=$(jq -r '.tool_input.command // empty' 2>/dev/null) || exit 0
[ -z "$cmd" ] && exit 0

# Explicit one-off — let it through untracked.
case "$cmd" in
  *"# one-off"*) exit 0 ;;
esac

# Commands that add global/system state worth making reproducible. Deliberately
# excludes local project deps (`npm install` / `pnpm add` without -g) — those
# belong in the project's package.json, not the dotfiles repo.
install_re='(^|[;&|[:space:]])('
install_re+='brew[[:space:]]+(install|tap)'
install_re+='|mise[[:space:]]+use'
install_re+='|(npm|pnpm|bun)[[:space:]]+(install|i|add)[^;&|]*(-g|--global)'
install_re+='|yarn[[:space:]]+global[[:space:]]+add'
install_re+='|(pipx|cargo|go|gem)[[:space:]]+install'
install_re+='|uv[[:space:]]+tool[[:space:]]+install'
install_re+='|(flatpak|winget)[[:space:]]+install'
install_re+=')'

if printf '%s' "$cmd" | grep -qE "$install_re"; then
  reason='This installs global/system state that should be reproducible across machines. Persist it via the /dotfiles workflow (it routes the change into chezmoi) rather than installing ad-hoc. ONLY if the user has explicitly asked for a throwaway one-off that should NOT be checked in, re-run the exact command with " # one-off" appended to bypass this guardrail.'
  jq -n --arg r "$reason" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $r
    }
  }'
fi

exit 0
