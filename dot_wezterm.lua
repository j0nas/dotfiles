local wezterm = require("wezterm")
local config = wezterm.config_builder()

-- Shell
if wezterm.target_triple:find("windows") then
  config.default_domain = "WSL:Ubuntu"
else
  config.default_prog = { "zsh" }
end

-- Font
config.font = wezterm.font("JetBrains Mono")
config.font_size = 14

-- Theme
config.color_scheme = "Catppuccin Mocha"

-- Window
config.window_padding = { left = 8, right = 8, top = 8, bottom = 8 }
config.hide_tab_bar_if_only_one_tab = true

return config
