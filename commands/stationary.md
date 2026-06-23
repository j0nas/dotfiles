---
name: stationary
description: Operate the Windows desktop "PomeloMadness" from this Mac over SSH — run commands, transfer files, pull from the seedbox. Key-only auth, LAN-scoped, sessions come in elevated.
argument-hint: "[what you want to do on the stationary]"
---

# Stationary (PomeloMadness) remote control

Drive the user's Windows 11 desktop **PomeloMadness** from the Mac over SSH. Work autonomously — discover, don't ask. The user never wants to touch PowerShell themselves; do it all from here.

## Connection

- **Host** `pomelomadness.local` (mDNS; last-seen LAN IP `192.168.50.48` as fallback). **Same LAN only** (firewall is LAN-scoped).
- **User** `jonas` (local admin). **Key** `~/.ssh/pomelomadness_ed25519` (ed25519, no passphrase — protected at rest by FileVault).
- Sessions come in **elevated** (the key lives in `administrators_authorized_keys`, so Windows hands an admin token) — service/registry/sshd_config edits work directly, no UAC dance.
- Default remote shell is **cmd.exe**. First connect to a new address: add `-o StrictHostKeyChecking=accept-new`.
- Base call: `ssh -i ~/.ssh/pomelomadness_ed25519 jonas@pomelomadness.local "<cmd>"`.

## Security model (keep it this way)

- **Key-only auth**: `PasswordAuthentication no` + `KbdInteractiveAuthentication no` in `C:\ProgramData\ssh\sshd_config`. Never re-enable passwords. Verify it's still off: a `-o PreferredAuthentications=password -o PubkeyAuthentication=no` attempt must return `Permission denied (publickey)`.
- **Firewall** rule `OpenSSH-Server-In-TCP` scoped to `192.168.50.0/24`. **sshd** StartType `Automatic`.
- Original config backed up at `C:\ProgramData\ssh\sshd_config.bak.preharden`.
- **Always validate sshd_config before restarting** — `& "$env:SystemRoot\System32\OpenSSH\sshd.exe" -t` must exit 0. A bad config + restart = permanent lock-out (key is the only way in). Restart via WMI (below) so the restart doesn't kill the session issuing it.
- **Teardown** (revoke all access): strip the Mac key line from `C:\ProgramData\ssh\administrators_authorized_keys`; `Stop-Service sshd; Set-Service sshd -StartupType Disabled`. Seedbox side: remove the `seedbox_pull` line from the seedbox's `~/.ssh/authorized_keys`.

## Running PowerShell — use base64, not quoting

cmd→PowerShell→bash quoting is unwinnable. **Always** pass PS as `-EncodedCommand`:
```bash
B64="$(iconv -f UTF-8 -t UTF-16LE /tmp/script.ps1 | base64 | tr -d '\n')"
ssh -i ~/.ssh/pomelomadness_ed25519 jonas@pomelomadness.local "powershell -NoProfile -EncodedCommand $B64" 2>/dev/null
```
- Write the PS to a local `/tmp/*.ps1`, encode, run — zero escaping.
- **Read output from files, not `2>&1`**: PowerShell serializes a native tool's stderr as CLIXML garbage. Redirect the tool's streams to a file (`& tool ... *> out.txt`) then `Get-Content` it, or have the script `Write-Output` only the wanted lines to stdout. Pipe the ssh call through `2>/dev/null` to drop CLIXML envelopes.
- `scp` to the box lands files in unexpected places — prefer writing files on the box via `Set-Content` inside an EncodedCommand.

## Long-running / detached jobs (must survive SSH disconnect)

Windows OpenSSH **kills every session-spawned process when the SSH session closes** — `Start-Process` and `start /b` do NOT survive. Reparent the job off the session via WMI:
```powershell
$r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = 'cmd.exe /c "C:\path\job.cmd"' }
# $r.ReturnValue 0 = ok ; $r.ProcessId = pid
```
- Bake the job into an **absolute-path** `.cmd` wrapper (no `%VAR%` reliance — context differs across launch methods) that writes a `--log-file`; poll with `Get-Content <log> -Tail`.
- Same trick restarts sshd without self-killing: WMI-spawn `powershell -Command "Restart-Service sshd -Force"`, then reconnect to verify.
- Verify a detached job actually survived by re-checking in a **fresh** session (`Get-Process`, log advancing).

## Pull a file/folder from the seedbox

The box pulls **directly** from the seedbox via rclone-over-sftp — nothing relays through the Mac. (Find/grab content on the seedbox first via `/seedbox`.)

- rclone at `C:\Users\jonas\rclone\rclone.exe`; config `%APPDATA%\rclone\rclone.conf` → remote `SB` (sftp, **key-based**, `known_hosts_file = C:/Users/jonas/.ssh/seedbox_known_hosts` pins the seedbox host key). **No seedbox password is ever stored on the box.**
  - Gotcha: pin the key that the connection actually presents — `ssh-keyscan j0nas.comet.usbx.me` — **not** the box's `/etc/ssh/ssh_host_*_key.pub`. USBx fronts SSH with a gateway whose host key differs from the inside-box file; pinning the file gives `knownhosts: key mismatch`.
- Seedbox key `C:\Users\jonas\.ssh\seedbox_pull` is **locked down on the seedbox side to read-only sftp** (its `authorized_keys` entry is `restrict,command="internal-sftp -R" …`; backup at `~/.ssh/authorized_keys.bak.preharden`) — a fully-compromised PomeloMadness can only *read* the seedbox, no shell, no writes (verified: read ✓, shell refused, mkdir refused). For rclone to work under that no-shell restriction the `SB` remote sets `shell_type = none` and `disable_hashcheck = true` (and `run_pull.cmd` also passes `--sftp-disable-hashcheck`).
- Launch the copy detached via WMI (above), into an absolute-path wrapper; `--exclude` what you don't need, `--transfers N` + `--multi-thread-streams N` for throughput. Bottleneck is the box's home-internet download speed, not either server.
- Lands in `C:\Users\jonas\Downloads\<name>`. Games are download-only here — the user installs locally.

## Task: $ARGUMENTS

Handle via SSH. Work autonomously — discover, don't ask. Confirm before destructive actions (deleting files, tearing down the SSH setup, re-enabling password auth).
