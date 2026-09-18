# PC side of the G9 desk — run from the Mac over SSH (see README.md). Not
# chezmoi-managed: from WSL this would need an elevated *interactive*
# scheduled task through a powershell.exe bridge, for one machine; the winget
# list alone would only install the binary. Re-run after a Windows reinstall.
#   - Deskflow via winget, same release as the Mac.
#   - Headless client settings in %APPDATA%\Deskflow\deskflow.conf.
#   - Logon scheduled task runs deskflow-core in the interactive session
#     (session-0/service processes cannot inject input). cmd.exe redirect for
#     the log: Deskflow's own log/toFile produced no file.
$ErrorActionPreference = "Stop"
$server = "Jonass-MacBook-Pro.local"   # the Mac's hostname; change with the Mac
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
level=4
"@ | Set-Content -Encoding ASCII "$dir\deskflow.conf"
$exe = "C:\Program Files\Deskflow\deskflow-core.exe"
$log = "$env:USERPROFILE\deskflow-client.log"
$a = New-ScheduledTaskAction -Execute "cmd.exe" -Argument "/c `"`"$exe`" client -s `"$dir\deskflow.conf`" $server >> `"$log`" 2>&1`""
$t = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$p = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Highest
$s = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName "Deskflow-Client" -Action $a -Trigger $t -Principal $p -Settings $s -Force | Out-Null
Get-Process deskflow-core -ErrorAction SilentlyContinue | Stop-Process -Force
Start-ScheduledTask -TaskName "Deskflow-Client"
