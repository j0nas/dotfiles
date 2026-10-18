# G9 desk — one hotkey moves keyboard, trackpad and the monitor between Mac and PC

Self-contained desk pairing. Everything lives here; the rest of the repo only
gates it (`g9_desk` flag) and symlinks two files into place.

## Topology
- **Mac** (MacBook Pro, Deskflow *server*, headless) — on the G9 via USB-C→HDMI = input **17**.
- **PomeloMadness** (Windows 11, Deskflow *client*, logon task) — on the G9 via DP1 = input **15**.
- **Monitor**: Samsung Odyssey G9 LC49G95T. No KVM; input switched over DDC/CI from the Mac (`m1ddc`).
- Hotkeys (server.conf): **Cmd+Alt+P** → PC, **Cmd+Alt+M** → Mac. Deskflow must own them: it is the only thing that still sees the keyboard while forwarding it.
- On the PC screen Cmd is sent as Ctrl (`modifierSuper=ctrl` under `[computer_PomeloMadness]` in settings.conf); Ctrl stays Ctrl, the Windows key is unreachable.
- Flow: hotkey → deskflow-core switches computer and runs its exit command (`g9 pc`) on leaving the Mac or its enter command (`g9 mac`) on coming back → m1ddc writes VCP 0x60.

## Files (this dir → `~/.config/g9-desk/`)
| file | role |
|---|---|
| `settings.conf` | deskflow-core settings (server mode): the computers (`[computer_<name>]` groups, with PomeloMadness's `modifierSuper=ctrl`) and the enter/exit commands that run `g9`. No comments, QSettings canonical layout — the core rewrites it on start; any other layout = chezmoi drift. `log/level` is a name (`Info`); an unknown value resets to Info. |
| `server.conf` | Hotkeys only: since 1.27 the core reads just the `keystroke`/`mousepress` lines here (a `section: screens` is skipped with a warning). No `links`: one shared monitor, edge-crossing would move input to an invisible computer. |
| `g9` | `g9 pc\|mac` → DDC input switch. Symlinked to `~/.local/bin/g9`. |
| `org.deskflow.server.plist` | LaunchAgent (KeepAlive) that runs deskflow-core directly; output goes to `~/Library/Logs/deskflow-server.log`. Symlinked into `~/Library/LaunchAgents/`. |
| `setup.sh` | Installs pinned Deskflow (not in Homebrew; sha256 from release `sums.txt`), (re)loads the agent. Run by `.chezmoiscripts/run_onchange_after_setup-g9-desk.sh.tmpl`. |
| `pomelomadness.ps1` | PC provisioning, run over SSH. Templated so winget installs the Mac's pinned version; the PC itself isn't chezmoi-managed (see file). |

Root-level pieces: `g9_desk` in `.chezmoi.toml.tmpl` (hardware-UUID hash match), `desk_g9:` in `.chezmoidata.yaml` (brews, Deskflow version+sha), ignore block in `.chezmoiignore`.

## Rebuild — new Mac
1. Put the new Mac's hardware-UUID hash in `.chezmoi.toml.tmpl` (`g9_desk`) — on that Mac: `ioreg -rd1 -c IOPlatformExpertDevice | awk -F'"' '/IOPlatformUUID/{printf "%s", $4}' | shasum -a 256` — and its Bonjour name (`scutil --get LocalHostName` + `.local`; not necessarily `hostname`, which is `Jonass-MBP` here and doesn't resolve) in `$server` in `pomelomadness.ps1`. `chezmoi init && chezmoi apply`.
2. System Settings → Privacy & Security → **Accessibility** → enable `deskflow-core` (listed after first launch; until then the agent loops with "assistive devices does not trust this process"). Releases from 1.27 are Developer ID signed, so the grant survives upgrades. A grant left from an ad-hoc build (1.26 and older) is pinned to that build's cdhash and toggling it changes nothing: `tccutil reset Accessibility org.deskflow.deskflow`, wait for the agent's next start, enable the new entry.
3. If the cable/port changed, re-find the input code: `m1ddc display 1 set input N` one code at a time with someone watching the screen (MCCS: DP1 15, DP2 16, HDMI1 17, HDMI2 18). Reads always return 0 on this monitor, so only eyes can confirm.

## Rebuild — reformatted PC
Prereq (not in this repo): key-only SSH into PomeloMadness as an admin (OpenSSH Server, key in `administrators_authorized_keys`, firewall rule LAN-scoped). Then from the Mac:
```bash
B64="$(iconv -f UTF-8 -t UTF-16LE ~/.config/g9-desk/pomelomadness.ps1 | base64 | tr -d '\n')"
ssh -i ~/.ssh/<key> jonas@pomelomadness.local "powershell -NoProfile -EncodedCommand $B64"
```
Registers logon task `Deskflow-Client` (interactive session — required to inject input) and starts it.

## Upgrading Deskflow
1. Read the release notes for config changes (1.27 moved the computers out of server.conf into settings.conf and made `log/level` a name).
2. Bump `deskflow_version` and `deskflow_sha256_macos_arm64` (from the release's `sums.txt`) under `desk_g9:` in `.chezmoidata.yaml`; `chezmoi apply` installs it via setup.sh and restarts the agent. Check `chezmoi status` is clean afterwards: a new release may rewrite settings.conf.
3. Re-run `pomelomadness.ps1` on the PC (Rebuild — reformatted PC, above): it installs the same version from winget, fails with an error if the PC doesn't end up on it, and restarts the client either way.

## Verify
```bash
pgrep -x deskflow-core | wc -l                      # 1
lsof -nP -iTCP:24800 -sTCP:ESTABLISHED               # one line from the PC
netstat -Lanp tcp | grep '\.24800 '                  # 0/0/3; a first number that stays above 0 = core not accepting
tail ~/Library/Logs/deskflow-server.log              # accepted client connection; a hotkey adds switch from … and g9: pc|mac
g9 pc; sleep 3; g9 mac                               # monitor flips and returns
```
Restart server: `launchctl kickstart -k gui/$(id -u)/org.deskflow.server` (PC reconnects in ~5 s).
Restart client (on PC): `Get-Process deskflow-core | Stop-Process -Force; Start-ScheduledTask Deskflow-Client`.
Client log: `C:\Users\jonas\deskflow-client.log`.

## Findings (verified 2026-09-18 — don't re-learn)
- G9 answers **no DDC reads** (all 0) but **honours writes**. Never `m1ddc chg` (reads 0, writes 0±n). Samsung-G7 codes 3/9 are no-ops here; forum "G9 has no DDC" claims are wrong for writes. The Mac's command still reaches the monitor while it shows the PC, so both directions run from the Mac.
- 1.26: `keystroke(key,<server>)` actions were dropped (PrimaryClient stub) and the macOS key table stopped at F16, so the switch was relayed by tailing the log. 1.27's enter/exit commands replace that relay.
- Hotkeys can't be tested with `osascript … key code`: synthetic events never reach the core's tap. Press them for real.
- The enter/exit commands fire when input actually changes computer, which isn't quite when the 1.26 relay fired. A hotkey for the computer you're already on runs nothing, so if the G9 shows the PC while input is on the Mac (a manual `g9 pc`, the monitor's own menu) run `g9 mac` or press Cmd+Alt+P then Cmd+Alt+M. In the other direction every return to the Mac now switches the monitor: the PC dropping off, the core stopping while on the PC, the Mac suspending while on the PC. So a manual or lid sleep of the Mac while you use the PC's own keyboard moves the G9 to the sleeping Mac (idle sleep is off here, `pmset` sleep 0). Deskflow has no hotkey action that runs a command, so this is the closest it gets.
- 1.26 (macOS) never finished a quit, SIGTERM included: the event thread stopped but the process kept 24800 open without accepting, silently, then deadlocked in `closeServer()` at the next AppKit event (`cocoa: release`). The server sat dead like that 2026-10-13 → 10-18 (PC log: endless `connecting to …`). 1.27 dropped the Cocoa loop: SIGTERM now exits in ~1 s and launchd restarts the core, PC back in ~5 s (verified 2026-10-18).
- 1.26 lost its event tap on every sleep (`ERROR: quartz event tap was disabled by user input` at suspend): hotkeys dead until the core restarted, so a wrapper script restarted it on the `INFO: resume` line. 1.27 runs the tap on its own thread and keeps it. A `pmset sleepnow` on 2026-10-19 logged no tap error, the same process served both hotkeys after wake, and the PC reconnected 2 s after resume, so the wrapper is gone. If the hotkeys die after a sleep again, that error line in the log is the tell. An HDMI replug is harmless; `g9` looks the monitor up by model each call anyway.
- Client resolves the Mac's `.local` to IPv6 link-local first: ~6 s to fall back to IPv4.
- TLS off: LAN-only, and the headless client has no dialog to trust a fingerprint. Anti-cheat games may reject Deskflow's synthetic input — use the PC's own keyboard there.
- The Windows installer also registers a `Deskflow` service (`deskflow-daemon.exe`, LocalSystem, auto start; there since the 1.26 install). It only launches a core once the GUI hands it a config file, which never happens here, so it idles next to the logon task.
- Fallback DDC from the PC: NirSoft ControlMyMonitor at `C:\Users\jonas\tools\ControlMyMonitor`; needs an interactive scheduled task (session 0 sees no monitor).
