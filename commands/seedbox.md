---
name: seedbox
description: Interact with your USBx seedbox — manage Sonarr (TV), Radarr (movies), Overseerr (requests), Bazarr (subtitles), Prowlarr (indexers), qBittorrent (downloads), and Plex.
argument-hint: "[what you want to do]"
---

# Seedbox Management

You are helping manage a USBx seedbox. You have full autonomous access via SSH and web APIs. Work independently — figure things out using the tools available rather than asking the user for information you can discover yourself.

## Credentials

Every service below needs auth. Before making any API call or SSH connection, **read `~/.claude/secrets/seedbox.json`** with the Read tool and pull values from it. The JSON schema is:

```json
{
  "ssh_host": "<seedbox hostname>",
  "ssh_username": "<your username>",
  "ssh_password": "...",
  "sonarr_api_key": "...",
  "radarr_api_key": "...",
  "overseerr_api_key": "...",
  "bazarr_api_key": "...",
  "plex_token": "...",
  "prowlarr_api_key": "...",
  "qbittorrent_username": "<your username>",
  "qbittorrent_password": "...",
  "ui_password": "..."
}
```

The file is decrypted by `chezmoi apply` from the age vault at `dot_claude/private_secrets/encrypted_private_seedbox.json.age`. If it's missing, run `chezmoi apply` (the SSH key configured in `.chezmoi.toml.tmpl` must be available). Never echo any of these values back to the user or write them to a file you're about to commit.

In every URL, code snippet, and path below, replace `<ssh_host>` and `<ssh_username>` with the actual values from the secrets file at runtime.

## Autonomous Operation

### SSH Access

Use `paramiko` for SSH (it's installed). `jq` is NOT available — use `python3` for JSON parsing. Load credentials from the secrets file:

```python
import json, os, paramiko
creds = json.load(open(os.path.expanduser('~/.claude/secrets/seedbox.json')))
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(
    creds['ssh_host'],
    username=creds['ssh_username'],
    password=creds['ssh_password'],
)
_, stdout, stderr = client.exec_command('your command here')
print(stdout.read().decode())
client.close()
```

For interactive/long-running commands use `invoke_shell()` and wait ~15s for output.

### USBx App Management

Services are managed with `app-<name>` commands (e.g. `app-radarr`, `app-plex`, `app-sonarr`):

- `app-radarr restart` / `stop` / `start`
- `app-radarr password <newpassword>` — reset UI password
- `app-plex claim` etc.

### Finding Things

- **Ports**: Check nginx proxy configs at `~/.apps/nginx/proxy.d/<service>.conf` for `proxy_pass` lines
- **Plex port**: Not in nginx — check `~/.config/plex/.../Preferences.xml` for `ManualPortMappingPort`, then test with curl
- **Sonarr/Radarr run in Docker** — use `172.17.0.1` instead of `localhost` when configuring connections from within those containers
- **Real home dir**: `/home30/<ssh_username>/` (not `/home/<ssh_username>/` which is a symlink)
- **Media**: `/home30/<ssh_username>/media/` (TV Shows, Movies, Music)
- **Downloads**: `/home30/<ssh_username>/downloads/qbittorrent/`

### JSON Parsing Pattern

```bash
curl -s 'URL' | python3 -c "import json,sys; data=json.load(sys.stdin); print(data['field'])"
```

## Services

| Service     | URL                                                                         | Secrets key                                     |
| ----------- | --------------------------------------------------------------------------- | ----------------------------------------------- |
| Sonarr      | `https://<ssh_host>/sonarr`                                                 | `sonarr_api_key`                                |
| Radarr      | `https://<ssh_host>/radarr`                                                 | `radarr_api_key`                                |
| Overseerr   | `https://<ssh_host>/overseerr`                                              | `overseerr_api_key`                             |
| Bazarr      | `https://<ssh_host>/bazarr`                                                 | `bazarr_api_key`                                |
| Plex        | `http://localhost:12625` (from host; `172.17.0.1:12625` from inside Docker) | `plex_token`                                    |
| Prowlarr    | `https://<ssh_host>/prowlarr`                                               | `prowlarr_api_key`                              |
| qBittorrent | `https://<ssh_host>/qbittorrent`                                            | `qbittorrent_username` + `qbittorrent_password` |
| SSH         | `<ssh_host>`                                                                | `ssh_username` + `ssh_password`                 |

The UI login (web dashboards) uses `ssh_username` + `ui_password`.

## API Reference

### Sonarr API (`/sonarr/api/v3/`)

Auth header: `X-Api-Key: <sonarr_api_key>`

- `GET /series` — list all shows
- `GET /series/lookup?term=NAME` — search for a show to add
- `POST /series` — add a show
- `GET /wanted/missing` — episodes not yet downloaded
- `GET /queue` — current download queue
- `GET /health` — system health
- `GET /qualityprofile` — available quality profiles
- `GET /rootfolder` — available root folders
- `GET /downloadclient` — download client config
- `GET /notification` — configured notifications

### Radarr API (`/radarr/api/v3/`)

Auth header: `X-Api-Key: <radarr_api_key>`

- `GET /movie` — list all movies
- `GET /movie/lookup?term=NAME` — search for a movie to add
- `POST /movie` — add a movie
- `GET /wanted/missing` — movies not yet downloaded
- `GET /queue` — current download queue
- `GET /health` — system health
- `GET /qualityprofile` — available quality profiles
- `GET /rootfolder` — available root folders

### Overseerr API (`/overseerr/api/v1/`)

- Auth header: `X-Api-Key: <overseerr_api_key>`
- `GET /request?take=20&skip=0` — list requests
- `GET /request?filter=pending` — pending requests
- `POST /request` — create a request (`mediaType: movie|tv`, `mediaId: TMDB_ID`)
- `GET /search?query=NAME` — search media
- `GET /user` — list users

### Bazarr API (`/bazarr/api/`)

- Auth header: `X-API-KEY: <bazarr_api_key>`
- The Bazarr API key (in `~/.apps/bazarr/config/config.yaml` under `auth.apikey` on the seedbox) is distinct from the UI login password
- `GET /system/status` — check Bazarr is up
- `GET /system/health` — health issues
- `GET /episodes` — episode subtitle status
- `GET /movies` — movie subtitle status
- `GET /providers` — configured subtitle providers

### Plex API (`http://localhost:12625/`)

- Always append `?X-Plex-Token=<plex_token>`
- `GET /library/sections` — list libraries (1=Movies, 2=TV Shows, 3=Music)
- `GET /library/sections/2/refresh` — trigger TV library scan
- `GET /library/sections/1/refresh` — trigger Movies library scan
- `GET /library/sections/2/all` — list all TV content

### Prowlarr API (`/prowlarr/api/v1/`)

Auth header: `X-Api-Key: <prowlarr_api_key>`

- `GET /indexer` — list indexers
- `POST /indexer` — add indexer (requires `appProfileId: 1`)
- `DELETE /indexer/{id}` — remove indexer
- `GET /indexer/schema` — all available indexer definitions (parse with python3)
- `GET /health` — system health

### qBittorrent API (`/qbittorrent/api/v2/`)

- Login first: `POST /auth/login` with `username=<qbittorrent_username>&password=<qbittorrent_password>`, save cookie
- `GET /torrents/info` — list all torrents
- `GET /torrents/info?filter=downloading` — active downloads
- `POST /torrents/pause` / `/torrents/resume` — pause/resume
- `POST /torrents/delete` — delete torrent (`deleteFiles=true/false`)
- `GET /transfer/info` — global transfer stats

```bash
# qBittorrent login pattern — pull creds from the secrets file first.
SECRETS=~/.claude/secrets/seedbox.json
QBT_USER=$(python3 -c "import json,os; print(json.load(open(os.path.expanduser('$SECRETS')))['qbittorrent_username'])")
QBT_PASS=$(python3 -c "import json,os; print(json.load(open(os.path.expanduser('$SECRETS')))['qbittorrent_password'])")
SSH_HOST=$(python3 -c "import json,os; print(json.load(open(os.path.expanduser('$SECRETS')))['ssh_host'])")
curl -s -c /tmp/qbt_cookies.txt -X POST \
  "https://${SSH_HOST}/qbittorrent/api/v2/auth/login" \
  -d "username=${QBT_USER}&password=${QBT_PASS}"
# Then use -b /tmp/qbt_cookies.txt for subsequent requests
```

Torrent states: `completed`, `stalledDL`, `forcedDL`, `downloading`, `pausedUP`

## Current Setup Notes

- **Download clients**: Both Sonarr and Radarr use qBittorrent via reverse proxy (host: `<ssh_host>`, port: `443`, SSL: on, URL base: `/qbittorrent`)
- **removeCompletedDownloads**: Enabled on both — downloads are auto-removed after import
- **Plex notifications**: Configured on both Sonarr and Radarr (uses `172.17.0.1:12625`)
- **Prowlarr active indexers**: The Pirate Bay, TorrentGalaxyClone, LimeTorrents, TorrentDownload, Knaben, showRSS, Torrent9
- **Cloudflare-blocked indexers**: EZTV, 1337x, KickassTorrents — blocked from seedbox IP, not worth adding
- **Subtitles**: Bazarr handles subtitles — do NOT enable `importExtraFiles` in Sonarr/Radarr
- **Hardlinks**: Enabled in Sonarr, but imports have been copying rather than hardlinking in practice
- **No recycle bin** configured — deletions are permanent

### Manual Import (Sonarr/Radarr)

- `GET /manualimport?folder=PATH` — scan a folder (use `/home/<ssh_username>/` Docker paths, not `/home30/<ssh_username>/`)
- `POST /manualimport` — send a **direct JSON array** (NOT wrapped in `{"files": [...]}`)
- Sonarr payload fields: `path`, `seriesId`, `episodeIds`, `quality`, `languages`, `releaseGroup`, `downloadId`, `importMode`
- Radarr payload fields: same but `movieId` instead of `seriesId`/`episodeIds`

## Task: $ARGUMENTS

Handle the user's request using SSH and/or the APIs above. Work autonomously — discover what you need rather than asking. Confirm before destructive actions (deleting files, removing shows/movies, etc.).
