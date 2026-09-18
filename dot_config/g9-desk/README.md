# G9 desk — one hotkey moves keyboard, trackpad and the monitor between Mac and PC

Self-contained desk pairing. Everything lives here; the rest of the repo only
gates it (`g9_desk` flag) and symlinks two files into place.

## Topology
- **Mac** (MacBook Pro, Deskflow *server*, headless) — on the G9 via USB-C→HDMI = input **17**.
- **PomeloMadness** (Windows 11, Deskflow *client*, logon task) — on the G9 via DP1 = input **15**.
- **Monitor**: Samsung Odyssey G9 LC49G95T. No KVM; input switched over DDC/CI from the Mac (`m1ddc`).
- Hotkeys (server.conf): **Cmd+Alt+P** → PC, **Cmd+Alt+M** → Mac. Deskflow must own them: it is the only thing that still sees the keyboard while forwarding it.
- Flow: hotkey → deskflow-core switches screen, logs `switch from "A" to "B"` → `deskflow-server` (wrapper) runs `g9 pc|mac` → m1ddc writes VCP 0x60.

## Files (this dir → `~/.config/g9-desk/`)
| file | role |
|---|---|
| `settings.conf` | deskflow-core settings (server mode). No comments, QSettings canonical layout — the core rewrites it on start; any other layout = chezmoi drift. `log/level` is an **index** (4 = INFO; a name parses as 0 = FATAL). |
| `server.conf` | Deskflow screens + hotkeys. No `links`: one shared monitor, edge-crossing would move input to an invisible screen. |
| `deskflow-server` | LaunchAgent entry point: runs the core, relays its switch lines to `g9`, owns the core's lifetime. |
| `g9` | `g9 pc\|mac` → DDC input switch. Symlinked to `~/.local/bin/g9`. |
| `org.deskflow.server.plist` | LaunchAgent (KeepAlive). Symlinked into `~/Library/LaunchAgents/`. |
| `setup.sh` | Installs pinned Deskflow (not in Homebrew; sha256 from release `sums.txt`), (re)loads the agent. Run by `.chezmoiscripts/run_onchange_after_setup-g9-desk.sh.tmpl`. |
| `pomelomadness.ps1` | PC provisioning, run over SSH. Not chezmoi-managed (see file). |

Root-level pieces: `g9_desk` in `.chezmoi.toml.tmpl` (hostname match), `desk_g9:` in `.chezmoidata.yaml` (brews, Deskflow version+sha), ignore block in `.chezmoiignore`.

## Rebuild — new Mac
1. Edit the hostname in `.chezmoi.toml.tmpl` (`g9_desk`) and `$server` in `pomelomadness.ps1`. `chezmoi init && chezmoi apply`.
2. System Settings → Privacy & Security → **Accessibility** → enable `deskflow-core` (listed after first launch; until then the agent loops with "assistive devices does not trust this process").
3. If the cable/port changed, re-find the input code: `m1ddc display 1 set input N` one code at a time with someone watching the screen (MCCS: DP1 15, DP2 16, HDMI1 17, HDMI2 18). Reads always return 0 on this monitor, so only eyes can confirm.

## Rebuild — reformatted PC
Prereq (not in this repo): key-only SSH into PomeloMadness as an admin (OpenSSH Server, key in `administrators_authorized_keys`, firewall rule LAN-scoped). Then from the Mac:
```bash
B64="$(iconv -f UTF-8 -t UTF-16LE ~/.config/g9-desk/pomelomadness.ps1 | base64 | tr -d '\n')"
ssh -i ~/.ssh/<key> jonas@pomelomadness.local "powershell -NoProfile -EncodedCommand $B64"
```
Registers logon task `Deskflow-Client` (interactive session — required to inject input) and starts it.

## Verify
```bash
pgrep -x deskflow-core | wc -l                      # 1
lsof -nP -iTCP:24800 -sTCP:ESTABLISHED               # one line from the PC
tail ~/Library/Logs/deskflow-server.log              # client "PomeloMadness" has connected; hotkey adds: switch from …
g9 pc; sleep 3; g9 mac                               # monitor flips and returns
```
Restart server: `launchctl kickstart -k gui/$(id -u)/org.deskflow.server` (PC reconnects in ~8 s).
Restart client (on PC): `Get-Process deskflow-core | Stop-Process -Force; Start-ScheduledTask Deskflow-Client`.
Client log: `C:\Users\jonas\deskflow-client.log`.

## Findings (verified 2026-09-18 — don't re-learn)
- G9 answers **no DDC reads** (all 0) but **honours writes**. Never `m1ddc chg` (reads 0, writes 0±n). Samsung-G7 codes 3/9 are no-ops here; forum "G9 has no DDC" claims are wrong for writes. The Mac's command still reaches the monitor while it shows the PC, so both directions run from the Mac.
- Deskflow `keystroke(key,<server>)` actions are dropped (PrimaryClient stub) and its macOS key table stops at F16 — no synthetic-key relay can work; hence the log-tail wrapper.
- deskflow-core **ignores SIGTERM**; an orphan keeps port 24800 and wedges on its dead stdout pipe, so replacements die with "already running" and the PC never reconnects. Wrapper `pkill -9`s stale cores on start and SIGKILLs its child on exit.
- Client resolves the Mac's `.local` to IPv6 link-local first: ~6 s to fall back to IPv4.
- TLS off: LAN-only, and the headless client has no dialog to trust a fingerprint. Anti-cheat games may reject Deskflow's synthetic input — use the PC's own keyboard there.
- Fallback DDC from the PC: NirSoft ControlMyMonitor at `C:\Users\jonas\tools\ControlMyMonitor`; needs an interactive scheduled task (session 0 sees no monitor).
