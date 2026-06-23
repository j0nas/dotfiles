---
name: seedbox
description: Interact with your USBx seedbox — Sonarr (TV), Radarr (movies), Seerr (requests), Bazarr (subtitles), Prowlarr (indexers), qBittorrent (downloads), Plex, Jellyfin, Audiobookshelf, plus ad-hoc Prowlarr→qBittorrent grabs for games/audiobooks/ebooks.
argument-hint: "[what you want to do]"
---

# Seedbox Management

USBx/Ultra.cc seedbox on `comet.usbx.me` (billed via Ultra.cc). Full autonomous SSH + API access — discover things yourself, don't ask. **Control panel** `https://cp.ultra.cc/` (login `ssh_username`+`ui_password`): billing, quota, expiration, app install/uninstall. CP tiles spinning forever = box unreachable (a health signal).

## Credentials

Read `~/.claude/secrets/seedbox.json` before any call. Keys: `ssh_host`, `ssh_username`, `ssh_password`, `ui_password`, `{sonarr,radarr,seerr,bazarr,prowlarr}_api_key`, `plex_token`, `qbittorrent_username`, `qbittorrent_password`, `jellyfin_password`. Web UI login = `ssh_username`+`ui_password`. Never echo or commit secrets. Missing file → `chezmoi apply` (decrypts `dot_claude/private_secrets/encrypted_private_seedbox.json.age`). Substitute `<ssh_host>`/`<ssh_username>` below.

## SSH

`paramiko` (installed), creds from secrets file. `jq` NOT available → parse JSON with `python3`. Long/interactive cmds: `invoke_shell()`, wait ~15s.

## Apps (`app-<name>`)

`app-<name> start|stop|restart|upgrade|backup|version`; `app-<name> password <pw>` (reset UI pw); `app-plex claim`.

**Install only via CP → Installers, never `app-<name> install` over SSH.** SSH/manual installs run but aren't registered with the panel (no CP tile/controls/auto-update, port hidden) — this is how Jellyfin got orphaned. Use the CLI only to *manage* CP-installed apps.
- **Is `<name>` managed?** `command -v app-<name>` (exists → managed; full list `ls /usr/bin/app-*`) and `app-ports show` (official app→port table, e.g. Jellyfin 12602).
- **Orphaned install** = on disk (`~/.apps/<name>`, `app-<name> version` works) but absent from CP → Apps. Fix: `app-<name> uninstall` (`--keep-config` keeps settings; `~/media` untouched) → reinstall via CP.

## Finding things

- **Ports**: `~/.apps/nginx/proxy.d/<service>.conf` → `proxy_pass`. Plex isn't in nginx → `~/.config/plex/.../Preferences.xml` `ManualPortMappingPort`.
- **Sonarr/Radarr/Seerr/Jellyfin are Docker.** Between-app connections use docker bridge `172.17.0.1:<port>/<urlbase>` (`12626/sonarr`, `12627/radarr`, `12631/bazarr`, Plex `12625`, Jellyfin `12602`) — never `localhost` (container loopback) or public host. Same ports on `127.0.0.1` also dodge the public proxy's query-param mangling.
- **Real home** `/home30/<ssh_username>/` (`/home/<ssh_username>/` is a symlink). **Media** `…/media/` (TV Shows, Movies, Music). **Downloads** `…/downloads/qbittorrent/`.

## Services

| Service | URL | Secrets |
|-|-|-|
| Sonarr | `https://<ssh_host>/sonarr` | `sonarr_api_key` |
| Radarr | `https://<ssh_host>/radarr` | `radarr_api_key` |
| Seerr | `https://<ssh_host>/seerr` | `seerr_api_key` |
| Bazarr | `https://<ssh_host>/bazarr` | `bazarr_api_key` |
| Prowlarr | `https://<ssh_host>/prowlarr` | `prowlarr_api_key` |
| qBittorrent | `https://<ssh_host>/qbittorrent` | `qbittorrent_username`+`_password` |
| Plex | `http://localhost:12625` (Docker `172.17.0.1:12625`) | `plex_token` |
| Jellyfin | `https://<ssh_host>/jellyfin` (internal `127.0.0.1:12602`) | `jellyfin_password` |
| Audiobookshelf | `https://audiobookshelf-<ssh_username>.comet.usbx.me/audiobookshelf` (internal `127.0.0.1:37600`) | `ui_password` (root user = `<ssh_username>`) |

## Docs (Context7)

Prefer Context7 over guessing endpoints/configs. IDs: Sonarr `/sonarr/sonarr` (+py `/devopsarr/sonarr-py`), Radarr `/websites/radarr_video_api` (+py `/devopsarr/radarr-py`), Seerr `/seerr-team/seerr` & `/websites/seerr_dev`, Pyarr `/totaldebug/pyarr`, TRaSH `/websites/trash-guides_info`.

## API cheat-sheet

Auth header `X-Api-Key: <…_api_key>` unless noted.

- **Sonarr** `/sonarr/api/v3/`: `GET /series`, `/series/lookup?term=`, `POST /series`; `GET /wanted/missing`, `/queue`, `/health`, `/qualityprofile`, `/rootfolder`, `/downloadclient`, `/notification`.
- **Radarr** `/radarr/api/v3/`: same shape — `/movie`, `/movie/lookup?term=`, `POST /movie`, `/wanted/missing`, `/queue`, `/health`, `/qualityprofile`, `/rootfolder`.
- **Seerr** `/seerr/api/v1/` (Overseerr-compatible; also `https://seerr-<ssh_username>.comet.usbx.me`): `GET /request[?filter=pending]`, `POST /request` (`mediaType:movie|tv`, `mediaId:TMDB_ID`), `GET /search?query=`, `/user`.
- **Bazarr** `/bazarr/api/` — header `X-API-KEY` (key in `~/.apps/bazarr/config/config.yaml` `auth.apikey`; internal `http://127.0.0.1:12631/bazarr/api`): `GET /system/status|health`, `/providers`; `/episodes?seriesid[]=<sonarrId>`, `/movies`, `/episodes/wanted`, `/movies/wanted`; `/system/languages/profiles` (profiles live in DB `table_languages_profiles`, NOT config.yaml); assign via `POST /series`(`seriesid`+`profileid`) / `POST /movies`(`radarrid`+`profileid`); `POST /system/tasks taskid=<id>` (`wanted_search_missing_subtitles_series|_movies`, `series_full_scan_subtitles`).
- **Prowlarr** `/prowlarr/api/v1/`: `GET /indexer`, `POST /indexer` (needs `appProfileId:1`), `DELETE /indexer/{id}`, `GET /indexer/schema`, `/health`.
- **Plex** `http://localhost:12625/` (append `?X-Plex-Token=<plex_token>`): `GET /library/sections` (1=Movies, 2=TV, 3=Music), `/library/sections/<id>/refresh`, `/library/sections/<id>/all`.
- **qBittorrent** `/qbittorrent/api/v2/`: `POST /auth/login` (`username=&password=`, save cookie + `Referer` header) first; `POST /torrents/add` (`urls=<magnet|.torrent URL>`, `category=`), `GET /torrents/info[?filter=downloading|category=<cat>]`, `POST /torrents/pause|resume|delete` (`deleteFiles=`), `GET /transfer/info`. States: completed, stalledDL, forcedDL, downloading, pausedUP.
- **Jellyfin** internal `127.0.0.1:12602/jellyfin`: `POST /Users/AuthenticateByName` `{"Username":<ssh_username>,"Pw":<jellyfin_password>}` + header `Authorization: MediaBrowser Client="..",Device="..",DeviceId="..",Version=".."` → then `Authorization: MediaBrowser Token="<token>"`. Libraries `GET/POST /Library/VirtualFolders` (dedupe by path, not name). Custom CSS = branding: `GET/POST /System/Configuration/branding` (`CustomCss`), served `GET /Branding/Css`. `jellyfin_password` == `ui_password`.

## Current setup

- **Download client**: Sonarr+Radarr → qBittorrent via reverse proxy (`<ssh_host>:443`, SSL, urlbase `/qbittorrent`). Completed downloads kept for seeding.
- **Plex notifications** on Sonarr+Radarr (`172.17.0.1:12625`).
- **Prowlarr indexers (7)**: The Pirate Bay, LimeTorrents, TorrentDownload, TorrentProject2, Knaben, showRSS, Torrent9. Cloudflare-blocked from seedbox IP (skip): EZTV, 1337x, KAT.
- **Scoring**: TRaSH unwanted formats (BR-DISK, LQ, x265-HD, 3D) at `-10000`; `minFormatScore=0`. **Recyclarr** (`~/bin/recyclarr`, cfg `~/.config/recyclarr/configs/main.yml`, weekly cron) adds preferred-release-group scores (WEB + HD-Bluray Tier 01-03) to the **Any** profile. Re-sync needs explicit service: `recyclarr sync sonarr -c <cfg>` then `radarr` (no-service form no-ops); use `--log debug`, verify via API.
- **Auto-import fix**: `~/scripts/autoimport_fix.py` (cron /15min) clears Sonarr/Radarr queue items stuck on "Automatic import is not possible" via manual-import by downloadId (`importMode=copy`); logs `~/scripts/autoimport_fix.log` (on action only). Self-heals.
- **Subtitles (Bazarr)**: English profile (id 1) on all + default. Do NOT enable `importExtraFiles` in Sonarr/Radarr (Bazarr owns subs). Only provider opensubtitlescom (free ~20/day; big backfills throttle 6h over days — add Gestdown/Podnapisi to speed up). Junk packs bundle `.srt`: hardlink `<base>.srt` → `<libdir>/<base>.en.srt`, then `series_full_scan_subtitles` for instant subs at zero quota.
- **Hardlinks** work (`copyUsingHardlinks` on; media+downloads share `/home30` fs; ~90% hardlinked, no 2× space). **No recycle bin** — deletions permanent.

### Jellyfin theming (web client only — never affects native apps)

Custom CSS = branding `CustomCss` (see API). Two traps that make a theme look dead:
1. **Branding CSS is cached hard** (PWA service worker + HTTP cache) — server-side changes don't show on a normal reload (browser re-injects the stale sheet). Verify only via incognito or DevTools → Application → unregister SW + Clear site data. With `/agent-browser` use a **fresh `--profile <new-path>`** each run; compare shades fast by live-overriding `document.documentElement.style.setProperty('--main-color','#…','important')`.
2. **Nested `@import`s don't recurse** — a top-level `@import` of a preset/wrapper loads, but its inner `@import`s don't fetch. Flatten: import each module directly. Catppuccin (`theme.css`+flavor) is flat; Ultrachromic presets are nested (use the module list).

Abandoned themes fail silently (CSS loads, selectors miss current markup) — check repo activity (JellySkin dead/2024/JF10.9; Catppuccin/Ultrachromic/Scyfin active). Current: Catppuccin Mocha + `--main-color:#8e5ae7` (lightened brand violet #421691; raw is unreadable on dark).

### Jellyfin Enhanced (plugin — Seerr-in-search, shortcuts; web-client only, SW-cached like CSS)

**Installed.** Canonical = Jellyfin Dashboard/API, NOT Ultra.cc CP. Add repos (`POST /Repositories`) then install (`POST /Packages/Installed/<Name>`) + `app-jellyfin restart`: File Transformation (`iamparadox.dev/jellyfin/plugins/manifest.json`, the server-side injection dep) + Jellyfin Enhanced (`raw.githubusercontent.com/n00bcodr/jellyfin-plugins/main/10.11/manifest.json`). Config `GET/POST /Plugins/f69e946a4b3c4e9a8f0a8d7c1b2c4d9b/Configuration` (POST replaces whole obj). Seerr fields: `JellyseerrEnabled`, `JellyseerrUrls`=PUBLIC `https://<ssh_host>/seerr` (direct internal port 307s→/login), `JellyseerrApiKey`.

**"unlinked" 404**: Seerr is Plex-mode (`mediaServerType=1`), unaware of Jellyfin users. Hybrid-link (keeps Plex intact): set `jellyfinUserId`=`6b29e78a7a6c4c9eaccbf177d0b8acd3` + `jellyfinUsername='j0nas'` on the Seerr `user` row, then restart jellyfin+seerr (clears 30-min user-id cache). DB = SQLite `~/.apps/seerr/db/db.sqlite3`; use `~/bin/sqlite3` (vendored 3.53.2 — `/usr/bin/sqlite3` is glibc-broken) or python3 `sqlite3`. Backup `db.sqlite3.bak.preJellyfinLink`. **Requests** go via the admin API key (no per-user sign-in), but Plex-mode Seerr rejects re-requests of already-tracked titles (`"No seasons available to request"`) while the UI still flashes a false "Requested" — only brand-new titles persist.

### Audiobookshelf (managed app — audiobooks/podcasts; separate from Jellyfin)

Install via CP. **Orphaned-container fix** (CP install fails `container name /audiobookshelf-<ssh_username> already in use`): `app-audiobookshelf uninstall` clears the squatted Docker name (users have no direct `docker` access), then reinstall via CP. Root user = `<ssh_username>` / `ui_password`. Library "Audiobooks" → `~/media/Audiobooks`, structure `Author/Title/<files>` (watcher auto-adds; multi-file = one book). Provider set to `audible`. No global "auto-match" toggle — untagged rips need a one-off match; files with embedded ASIN/ISBN match on scan.
**Add a book**: see *Ad-hoc grab* below — hardlink into `~/media/Audiobooks/<Author>/<Title>/`, auto-appears, then match for cover/metadata.
**API** `127.0.0.1:37600`: `POST /login {username,password}` → `user.token` → header `Authorization: Bearer <token>`. `GET /api/libraries[/{id}/items]`, `POST /api/libraries/{id}/scan`, `POST /api/items/{id}/match {provider:audible,title,author}` (applies best match). DB `~/.apps/audiobookshelf/config/absdatabase.sqlite` (`libraries`/`libraryFolders`/`users`).

### Ad-hoc grab (no *arr — games, audiobooks, ebooks, comics, music)

For media with no request/automation layer. Decided NOT worth dedicated software (Questarr for games, LazyLibrarian for books both exist + plug into Prowlarr+qBittorrent, but Questarr isn't on the Ultra.cc CP / no Docker access, and a seedbox can't *play* games anyway — so drive it by hand):
1. **Search** `GET /prowlarr/api/v1/search?query=<term>&type=search&limit=100` (`X-Api-Key`). Fields: `title`, `seeders`, `size`, `categories`, `downloadUrl`|`magnetUrl`|`infoHash`. Sort by seeders; pick a trusted source at the right version. Games: trusted repackers FitGirl/DODI, scene RUNE/FLT/CODEX — avoid unlabeled re-uploads for anything you'll execute.
2. **Quota** big grabs first: `quota -s` (limit 3725G; box `/home30` 17T).
3. **Add**: qBittorrent login → `POST /torrents/add` `urls=<downloadUrl|magnet>&category=<games|audiobooks|...>`. A non-*arr `category` keeps it out of Sonarr/Radarr import paths; it lands in `~/downloads/qbittorrent/`.
4. **Land it**:
   - **Games**: stay in downloads — box is download-only, pull to local PC via SFTP or `rsync -avP <ssh_username>@<ssh_host>:'~/downloads/qbittorrent/<name>' .`; install locally.
   - **Audiobooks/ebooks**: hardlink completed files into the library (`~/media/Audiobooks/<Author>/<Title>/`) — keeps seeding, ABS auto-adds, then match (see Audiobookshelf).

### Manual import (Sonarr/Radarr)

Cron handles the common "matched by ID" stall; by hand, against internal (`127.0.0.1:12626/sonarr`, `12627/radarr`):
- **Scan** `GET /api/v3/manualimport?downloadId=<HASH>&filterExistingFiles=true`. **Never add `seriesId`/`movieId`** (returns the library's existing files, ignores the download). `folder=<path>` works if percent-encoded (`%20` not `+`, Docker `/home/<ssh_username>/` paths).
- **Import** `POST /api/v3/command` `{"name":"ManualImport","importMode":"copy","files":[{path,seriesId|movieId,episodeIds,quality,languages,releaseGroup,downloadId,indexerFlags}]}`. `copy`=hardlink (keeps seed). Only import items with zero `rejections`; `downloadId`=torrent hash from queue.

## Task: $ARGUMENTS

Handle via SSH/APIs above. Work autonomously — discover, don't ask. Confirm before destructive actions (deleting files, removing media).
