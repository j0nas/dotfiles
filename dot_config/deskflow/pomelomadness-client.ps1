# PomeloMadness side of the Deskflow setup — reference copy, run from the Mac
# over SSH (see the `stationary` skill: pass it as -EncodedCommand). Applied
# 2026-09-18; re-run to re-provision after a Windows reinstall.
#
#   - Deskflow via winget (pinned to the same release as the Mac).
#   - Headless client settings in %APPDATA%\Deskflow\deskflow.conf.
#   - A logon scheduled task runs deskflow-core as the interactive user; a
#     service/session-0 process could not inject input into the desktop.
#     The client retries the server on its own, so no KeepAlive logic needed.
#
# The client first tries the Mac's IPv6 link-local address and only falls back
# to IPv4 after two 3 s timeouts, so a fresh connect takes ~6 s. Harmless.
$ErrorActionPreference = "Stop"
$server = "Jonass-MacBook-Pro.local"
& winget install --id Deskflow.Deskflow --version 1.26.0 --source winget --accept-package-agreements --accept-source-agreements --silent *> "$env:USERPROFILE\deskflow_install.log"
$dir = "$env:APPDATA\Deskflow"
New-Item -ItemType Directory -Force $dir | Out-Null
@"
[core]
computerName=PomeloMadness
[client]
remoteHost=$server
[security]
tlsEnabled=false
[log]
level=INFO
"@ | Set-Content -Encoding ASCII "$dir\deskflow.conf"
$exe = "C:\Program Files\Deskflow\deskflow-core.exe"
$a = New-ScheduledTaskAction -Execute $exe -Argument "client -s `"$dir\deskflow.conf`" $server"
$t = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$p = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Highest
$s = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName "Deskflow-Client" -Action $a -Trigger $t -Principal $p -Settings $s -Force | Out-Null
Get-Process deskflow-core -ErrorAction SilentlyContinue | Stop-Process -Force
Start-ScheduledTask -TaskName "Deskflow-Client"
