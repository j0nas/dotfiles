#!/bin/bash
# mise postinstall hook for herdr (dot_config/mise/config.toml). mise runs it
# after every install/upgrade, with that version already on PATH.
set -e

# herdr's docs ask for the integration to be reinstalled after each upgrade.
# It writes ~/.claude/hooks/herdr-agent-state.sh and its SessionStart entry in
# ~/.claude/settings.json, which dot_claude/modify_settings.json carries over.
# The hook tells herdr which Claude session runs in a pane, so herdr can resume
# it after a server restart; Claude's working/blocked state comes from herdr's
# screen detection either way.
herdr integration install claude
