# dotfiles

Personal macOS config.

## Setup on a new Mac

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/j0nas/dotfiles/main/setup.sh)"
```

Run it exactly like that: piped into bash, Homebrew's installer has no terminal to ask for the password in. It installs Homebrew (and with it Apple's command line tools), age and mise, then applies this repo. It is safe to run again.

### New Mac checklist

What the script can't do:

1. Run `gh auth login`. Git uses it to push, and to clone private repos.
2. Grant permissions in System Settings → Privacy & Security:
   - Accessibility: AeroSpace, Tinycast
   - Full Disk Access: Tinycast (file search), WezTerm (cursor size and color). After granting WezTerm, run `bash ~/.local/share/chezmoi/.chezmoiscripts/run_onchange_macos-defaults.sh` in it, since the apply already skipped that part.
3. Log out and back in. Key repeat and the Spaces setting only apply then.
4. Sign in: `claude` (then `/login`), iCloud (the Obsidian vault comes with it, plugins included), Brave and the other apps.
5. Secrets (`~/.claude/secrets`) are skipped on a Mac that can't decrypt them. To add this one, run `ssh-keygen -t ed25519` and follow the header of `.age-recipients` on a Mac that can.
6. Fusion 360 comes from Autodesk and HueForge from its store; `chezmoi apply` then adds the Fusion add-in and installs HueForge from the zip in `~/Downloads`.
7. Epson ET-8550: install the Drivers and Utilities Combo Package from [epson.com](https://epson.com/Support/Printers/All-In-Ones/ET-Series/Epson-ET-8550/s/SPT_C11CJ21201). Epson's CDN blocks scripts. AirPrint alone breaks ICC profiles and custom paper sizes, and Epson Print Layout needs the driver.
8. The Mac at the G9 desk: `dot_config/g9-desk/README.md`.

## How it's wired

[chezmoi](https://www.chezmoi.io/) owns the dotfiles. This repo is the source, cloned to `~/.local/share/chezmoi`; files prefixed `dot_` map to `~/.*`, and `chezmoi apply` writes them.

[mise](https://mise.jdx.dev/) owns CLI tools (`dot_config/mise/config.toml`), chezmoi included. They upgrade weekly within their pins, to releases at least 7 days old. Claude Code uses Anthropic's installer, so its own updater works.

Homebrew apps and VS Code extensions are listed in `.chezmoidata.yaml`. Both lists are declarative: the weekly apply uninstalls what isn't on them.

zsh plugins are pinned, checksummed archives in `.chezmoiexternal.toml`.

## Daily use

| Task | How |
|---|---|
| Try a tool once | `brew install <x>` or `mise x <tool> -- <cmd>`; the weekly apply removes it. |
| Keep an app or VS Code extension | Add it to `.chezmoidata.yaml` → `chezmoi apply`. |
| Add a CLI tool | `mise use -g <tool>@latest`, then commit `dot_config/mise/config.toml`. |
| Add a zsh plugin | Add its archive URL + sha256 to `.chezmoiexternal.toml`, source it at the end of `dot_zshrc` → `chezmoi apply`. |
| Pull a dotfile in | `chezmoi add ~/.foo` (`--template` if it has personal data). |
| Edit a managed dotfile | `chezmoi edit ~/.foo`, or edit it under `~/.local/share/chezmoi/`. |
| Push changes | `git commit` in the source dir; the post-commit hook pushes. |
