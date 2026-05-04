# dotfiles

Personal config for macOS, Windows (WSL), and Linux.

## Setup on a new machine

Windows: first install WSL from PowerShell with `wsl --install -d Ubuntu`.

Then, on any platform:

```sh
curl -fsLS https://raw.githubusercontent.com/j0nas/dotfiles/master/setup.sh | bash
```

## How it's wired

**[chezmoi](https://www.chezmoi.io/)** owns dotfiles. This repo is the source of truth, cloned to `~/.local/share/chezmoi`. Files prefixed `dot_` map to `~/.*` after templates render with per-machine data; `chezmoi apply` propagates changes.

**[mise](https://mise.jdx.dev/)** owns CLI tool versions (node, gh, starship, zoxide, fzf, claude). Tool list: `dot_config/mise/config.toml`.

**[antidote](https://github.com/mattmc3/antidote)** is the zsh plugin manager; plugins listed in `dot_zsh_plugins.txt`.

**GUI app + VS Code extension lists** live in `.chezmoidata.yaml`. Two `run_onchange_*` scripts consume them: `install-packages.sh.tmpl` (brew bundle on macOS with cleanup, winget on Windows, flatpak on Linux) and `install-vscode-extensions.sh.tmpl`. Edit YAML → `chezmoi apply`.

## Daily use

| Task | How |
|---|---|
| Add a GUI app | Add the per-platform IDs to `.chezmoidata.yaml` → `chezmoi apply`. IDs: brew.sh/cask, winstall.app, flathub.org. |
| Add a VS Code extension | Add to `vscode_extensions:` in `.chezmoidata.yaml` → `chezmoi apply`. |
| Add a CLI tool | `mise use -g <tool>@latest`, then commit `dot_config/mise/config.toml`. |
| Add a zsh plugin | Edit `dot_zsh_plugins.txt` → `chezmoi apply`. |
| Pull a dotfile in | `chezmoi add ~/.foo` (`--template` if it has personal data). |
| Edit a managed dotfile | `chezmoi edit ~/.foo` (or edit directly under `~/.local/share/chezmoi/`). |
| Push changes | `git commit` from the source dir — the post-commit hook auto-pushes. |

**`brew bundle` and VS Code extensions are strict declarative state**: anything installed locally but missing from `.chezmoidata.yaml` gets uninstalled on next apply. Add it to the YAML or it goes away. `winget` and `flatpak` are install-only — remove those manually.

Example — adding Slack:

```yaml
# in packages.darwin.casks
  - slack
# in packages.windows.winget
  - SlackTechnologies.Slack
# in packages.linux.flatpaks
  - com.slack.Slack
```

## Manual steps (not automated)

- **Epson ET-8550 printer drivers** — install the *Drivers and Utilities Combo Package* manually from [epson.com/Support/.../ET-8550](https://epson.com/Support/Printers/All-In-Ones/ET-Series/Epson-ET-8550/s/SPT_C11CJ21201). Required for color-accurate printing — AirPrint alone breaks ICC profiles, custom paper sizes, and Epson Print Layout. Epson's CDN blocks scripted access (Incapsula WAF), and the Apple-bundled driver pack is pinned to macOS 11. The `epson-print-layout` cask installs the layout app declaratively but only functions once the official driver is present.

## Obsidian + iCloud notes

The vault lives in iCloud. Each platform reaches it differently:

- **macOS** — iCloud Drive is native. Vault path: `~/Library/Mobile Documents/iCloud~md~obsidian/Documents/Jonas' Vault`.
- **Windows (WSL)** — `run_onchange_install-packages.sh.tmpl` runs `winget install Apple.iCloud`. Sign in and tick *iCloud Drive*; the vault syncs to `C:\Users\<user>\iCloudDrive\iCloud~md~obsidian\Jonas' Vault`. Re-run `chezmoi apply` once sync completes so the plugin script can find it.
- **Linux** — no official iCloud client. Use Obsidian Sync, Syncthing, or git for cross-device sync.

`run_onchange_setup-obsidian-tasks.sh.tmpl` drops the pinned [Tasks](https://github.com/obsidian-tasks-group/obsidian-tasks) plugin into the vault and enables it. Appends rather than replaces, so existing plugins survive. Bump `PLUGIN_VERSION` to upgrade.
