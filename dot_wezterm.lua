local wezterm = require("wezterm")
local config = wezterm.config_builder()
local act = wezterm.action

-- Shell
config.default_prog = { "zsh" }

-- Font
config.font = wezterm.font("JetBrains Mono")
config.font_size = 14

-- macOS: let both Option keys compose so Nordic specials (|, \, @, etc.) work
config.send_composed_key_when_left_alt_is_pressed = true
config.send_composed_key_when_right_alt_is_pressed = true

-- Theme
config.color_scheme = "Catppuccin Mocha"

-- Cursor accent overrides the colorscheme's cursor only.
config.colors = {
  cursor_bg = "#421691",
  cursor_fg = "#ffffff",
  cursor_border = "#421691",
}

-- Window
config.window_padding = { left = 8, right = 8, top = 8, bottom = 8 }
config.hide_tab_bar_if_only_one_tab = true

-- Keys (tmux-inspired)
config.keys = {
  { key = "h", mods = "CTRL|SHIFT", action = act.SplitHorizontal({ domain = "CurrentPaneDomain" }) },  -- sc: Ctrl+Shift+H | Split pane horizontally | Panes
  { key = "v", mods = "CTRL|SHIFT", action = act.SplitVertical({ domain = "CurrentPaneDomain" }) },  -- sc: Ctrl+Shift+V | Split pane vertically | Panes
  { key = "w", mods = "CTRL|SHIFT", action = act.CloseCurrentPane({ confirm = true }) },  -- sc: Ctrl+Shift+W | Close current pane | Panes
  { key = "t", mods = "CTRL|SHIFT", action = act.SpawnTab("CurrentPaneDomain") },  -- sc: Ctrl+Shift+T | New tab | Tabs
  { key = "LeftArrow", mods = "CTRL|SHIFT", action = act.ActivatePaneDirection("Left") },  -- sc: Ctrl+Shift+←/→/↑/↓ | Focus pane in direction | Panes
  { key = "RightArrow", mods = "CTRL|SHIFT", action = act.ActivatePaneDirection("Right") },
  { key = "UpArrow", mods = "CTRL|SHIFT", action = act.ActivatePaneDirection("Up") },
  { key = "DownArrow", mods = "CTRL|SHIFT", action = act.ActivatePaneDirection("Down") },
  -- The new window takes focus, so Cmd+Alt+Shift+N (AeroSpace) can fling it straight to a workspace.
  { key = "o", mods = "CTRL|SHIFT", action = act.PaneSelect({ mode = "MoveToNewWindow" }) },  -- sc: Ctrl+Shift+O | Pop a pane out to its own window (pick by letter) | Panes
}

return config
