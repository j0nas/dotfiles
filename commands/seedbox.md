---
name: seedbox
description: Interact with your USBx seedbox — manage Sonarr (TV), Radarr (movies), Seerr (requests), Bazarr (subtitles), Prowlarr (indexers), qBittorrent (downloads), and Plex.
argument-hint: "[what you want to do]"
---

# Seedbox Management

You are helping manage a USBx seedbox (USBx and Ultra.cc are the same provider — the box is on `comet.usbx.me`, billed/managed through Ultra.cc). You have full autonomous access via SSH and web APIs. Work independently — figure things out using the tools available rather than asking the user for information you can discover yourself.

**Provider control panel**: `https://cp.ultra.cc/` — for account/billing, expiration date, disk/traffic quota, and service-level controls that aren't exposed via `app-*` commands on the box. The CP fetches live data from the box itself, so if the box is unreachable the dashboard tiles will spin forever (a useful health signal in its own right). Login is the same `ssh_username` + `ui_password` from the secrets file.

## Credentials

Every service below needs auth. Before making any API call or SSH connection, **read `~/.claude/secrets/seedbox.json`** with the Read tool and pull values from it. The JSON schema is:

```json
{
  "ssh_host": "<seedbox hostname>",
  "ssh_username": "<your username>",
  "ssh_password": "...",
  "sonarr_api_key": "...",
  "radarr_api_key": "...",
  "seerr_api_key": "...",
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

**Installing apps — route through the CP, never `app-<name> install` over SSH.** Ultra.cc-managed apps must be installed from the **Control Panel → Installers** tab (green **Install** button), not via SSH or any hand-rolled Docker/binary setup. An SSH/manual install *runs* but is **never registered with the panel**: it won't show under CP → Apps, gets no CP start/stop/upgrade buttons and no auto-update toggle, and the panel can't report its port (you'll have to dig it out of nginx/`ps`). This is exactly how Jellyfin ended up orphaned. Use the `app-<name>` CLI only to *manage* an app already CP-installed (start/stop/restart/upgrade/password/backup), never for the first install.

**Automated check — is `<name>` Ultra.cc-managed?** (box-side, no panel access needed)

- `command -v app-<name>` → if the CLI exists in `/usr/bin`, the app is Ultra.cc-managed (81-app catalog; list all with `ls /usr/bin/app-* | sed 's#.*/app-##' | sort`).
- `app-ports show` → authoritative table of every official app + its reserved port (e.g. Jellyfin → 12602). Check one: `app-ports show | grep -i <name>`.

Decision flow before any install: if `command -v app-<name>` succeeds → it's managed → have the user click **Install** in the CP (a fresh CP install regenerates the nginx proxy and assigns the port); do **not** install it yourself. Spotting an **orphaned** install: it's on disk (`~/.apps/<name>` exists, `app-<name> version` returns a version) but absent from CP → Apps. Fix by `app-<name> uninstall` (add `--keep-config` to preserve settings; `~/media` is never touched), then reinstall via the CP.

### Finding Things

- **Ports**: Check nginx proxy configs at `~/.apps/nginx/proxy.d/<service>.conf` for `proxy_pass` lines
- **Plex port**: Not in nginx — check `~/.config/plex/.../Preferences.xml` for `ManualPortMappingPort`, then test with curl
- **Sonarr/Radarr/Seerr run in Docker containers** — when configuring connections *between* them (e.g. Seerr → Radarr/Sonarr), use the docker bridge `172.17.0.1:<port>/<urlbase>` (e.g. `172.17.0.1:12626/sonarr`, `172.17.0.1:12627/radarr`, `172.17.0.1:12631/bazarr`; Plex `12625`), never `localhost` (that's the container's own loopback) or the public hostname. These same internal ports on `127.0.0.1` dodge the public proxy's query-param mangling.
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
| Seerr       | `https://<ssh_host>/seerr`                                                  | `seerr_api_key`                                 |
| Bazarr      | `https://<ssh_host>/bazarr`                                                 | `bazarr_api_key`                                |
| Plex        | `http://localhost:12625` (from host; `172.17.0.1:12625` from inside Docker) | `plex_token`                                    |
| Prowlarr    | `https://<ssh_host>/prowlarr`                                               | `prowlarr_api_key`                              |
| qBittorrent | `https://<ssh_host>/qbittorrent`                                            | `qbittorrent_username` + `qbittorrent_password` |
| SSH         | `<ssh_host>`                                                                | `ssh_username` + `ssh_password`                 |

The UI login (web dashboards) uses `ssh_username` + `ui_password`.

## Documentation (Context7)

Context7 has authoritative, up-to-date docs for this stack — prefer it over guessing endpoints or hand-tuning configs. The hand-maintained API reference below is a quick cheat sheet; Context7 is ground truth. Resolve with `mcp__context7__query-docs` using these library IDs:

| Topic | Library ID | Use for |
| ----- | ---------- | ------- |
| Seerr (repo) | `/seerr-team/seerr` | Request-manager internals, troubleshooting (e.g. download tracker), notifications |
| Seerr (docs site) | `/websites/seerr_dev` | Setup, settings, migration, reverse-proxy guidance (`docs.seerr.dev`) |
| Sonarr | `/sonarr/sonarr` · `/devopsarr/sonarr-py` | API v3 endpoints + Python client |
| Radarr | `/websites/radarr_video_api` · `/devopsarr/radarr-py` | API docs + Python client |
| Pyarr | `/totaldebug/pyarr` | One client covering Sonarr/Radarr/Prowlarr/Bazarr/Lidarr |
| TRaSH Guides | `/websites/trash-guides_info` | Canonical quality-profile, naming, and hardlink/config tuning for the *arr family |

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

### Seerr API (`/seerr/api/v1/`)

Request manager (Overseerr-compatible `/api/v1/`). Also at the subdomain `https://seerr-<ssh_username>.comet.usbx.me`. Managed via the CP or `app-seerr` (start/stop/restart/upgrade/backup/version).

- Auth header: `X-Api-Key: <seerr_api_key>`
- `GET /request?take=20&skip=0` — list requests
- `GET /request?filter=pending` — pending requests
- `POST /request` — create a request (`mediaType: movie|tv`, `mediaId: TMDB_ID`)
- `GET /search?query=NAME` — search media
- `GET /user` — list users

### Bazarr API (`/bazarr/api/`)

- Auth header: `X-API-KEY: <bazarr_api_key>` (key in `~/.apps/bazarr/config/config.yaml` `auth.apikey`; distinct from UI password). Internal endpoint `http://127.0.0.1:12631/bazarr/api`.
- `GET /system/status` · `/system/health` · `/providers` — up / health issues / providers
- `GET /episodes?seriesid[]=<sonarrId>` · `/movies` — subtitle status; `/episodes/wanted` · `/movies/wanted` — missing-subtitle lists
- `GET /system/languages/profiles` — language profiles (stored in the DB `table_languages_profiles`, **not** config.yaml — its `languages:` section is empty). Assign: `POST /series` (`seriesid`+`profileid`) / `POST /movies` (`radarrid`+`profileid`)
- `POST /system/tasks` `taskid=<id>` — run a task: `wanted_search_missing_subtitles_series`/`_movies` (search missing), `series_full_scan_subtitles` (index existing on-disk subs)

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
- **Completed downloads**: retained for seeding (not auto-removed) — see Hardlinks
- **Plex notifications**: Configured on both Sonarr and Radarr (uses `172.17.0.1:12625`)
- **Prowlarr active indexers** (7): The Pirate Bay, LimeTorrents, TorrentDownload, TorrentProject2, Knaben, showRSS, Torrent9 (TorrentGalaxyClone removed — TGx is defunct)
- **Custom formats / scoring**: TRaSH "unwanted" formats (BR-DISK, LQ, x265-HD, 3D) at `-10000` block junk; `minFormatScore=0`. **Recyclarr** (`~/bin/recyclarr`, config `~/.config/recyclarr/configs/main.yml`, weekly cron) adds positive "preferred release group" scores (WEB + HD-Bluray Tier 01-03) to the **Any** profile (≈all media uses it) so clean releases beat anonymous junk packs. Re-sync needs an explicit service arg — `recyclarr sync sonarr -c <cfg>` then `radarr` (the no-service form silently no-ops); INFO output is near-silent, use `--log debug` and verify via API.
- **Auto-import fix**: `~/scripts/autoimport_fix.py` (cron, every 15 min) auto-resolves Sonarr/Radarr queue items stuck on "Automatic import is not possible" (junk season-packs matched by ID) via manual-import by downloadId, `importMode=copy`. Logs to `~/scripts/autoimport_fix.log` (on action only) — these stalls now self-heal, don't expect them to sit.
- **Cloudflare-blocked indexers**: EZTV, 1337x, KickassTorrents — blocked from seedbox IP, not worth adding
- **Subtitles (Bazarr)**: English profile (id 1) assigned to all shows+movies and set as serie/movie default. Do NOT enable `importExtraFiles` in Sonarr/Radarr — Bazarr owns subs. Only provider is **opensubtitlescom (free, ~20 downloads/day)** — big backfills throttle 6h and finish over days; add providers (Gestdown, Podnapisi) to speed up. Junk packs often bundle `.srt`: hardlink `<base>.srt` from the download folder to `<libdir>/<base>.en.srt`, then run the `series_full_scan_subtitles` task for instant subs at zero quota.
- **Hardlinks**: working — `copyUsingHardlinks` on in both, and media + downloads share one filesystem (`/home30`), so imports hardlink (verified ~90% of library files hardlinked to their seeding copy — no 2× space)
- **No recycle bin** configured — deletions are permanent

### Manual Import (Sonarr/Radarr)

The autoimport cron handles the common "matched by ID" block; to do it by hand:

- **Scan**: `GET /api/v3/manualimport?downloadId=<HASH>&filterExistingFiles=true`. **Do NOT add `seriesId`/`movieId`** — that ignores the download and returns the series' *existing* library files instead. `folder=<path>` also works but percent-encode it (`%20`, not `+`) and use Docker `/home/<ssh_username>/` paths. Run against the internal endpoint (`127.0.0.1:12626/sonarr`, `12627/radarr`).
- **Import**: `POST /api/v3/command` with `{"name":"ManualImport","importMode":"copy","files":[{path, seriesId|movieId, episodeIds, quality, languages, releaseGroup, downloadId, indexerFlags}]}`. `importMode:"copy"` = hardlink (keeps the seed). Only import items mapped to an episode/movie with zero `rejections`. Get `downloadId` (the torrent hash) from the queue record.

## Task: $ARGUMENTS

Handle the user's request using SSH and/or the APIs above. Work autonomously — discover what you need rather than asking. Confirm before destructive actions (deleting files, removing shows/movies, etc.).
