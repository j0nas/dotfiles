# Obsidian CLI — reference for obsidian-gtd

Trimmed reference covering the handful of Obsidian CLI (v1.12+) commands
this skill actually uses, plus a few more listed as future-facing. Not a
full manual — for anything not here, run `obsidian help <cmd>` against a
real CLI. The CLI is young and evolving, so treat this file as the
best-effort cached lookup, not gospel.

## Prerequisites

- **Obsidian desktop must be running.** The CLI is a remote control over
  IPC, not a headless tool. First call auto-launches the app if it's not
  already up — that side effect is why this skill doesn't route capture
  or Inbox processing through the CLI.
- CLI targets the currently active vault by default. Multi-vault syntax
  (`obsidian "Vault Name" <cmd>`) exists but is flaky on some environments;
  safer to switch vaults in the UI first.

## Syntax rules

```
obsidian <command>[:subcommand] [key=value ...] [flags]
```

- All parameters use `key=value`. Quote values with spaces:
  `content="hello world"`.
- Vault-relative paths throughout — no leading slash, no absolute paths.
- `.md` is explicit in `path=` / `to=` for `move`, `read`, `append`; but
  `create` appends it automatically and requires you to omit it. Yes, this
  is inconsistent — double-check per command.

## `move` — the one command this skill actually depends on

```bash
obsidian move path="Projects/Florence.md" to="Archive/Florence.md"
obsidian move path="Someday-Maybe/Florence.md" to="Projects/Florence.md"
```

- `path=` — current vault-relative path, including `.md`.
- `to=` — new vault-relative path, including `.md`.
- Handles move, rename, or both in one call.
- **Rewrites inbound `[[wikilinks]]` across the vault.** This is the entire
  reason the Weekly Review routes archive and Someday-Maybe activation
  through the CLI instead of a filesystem `mv` — a raw move would silently
  break every `[[<name>]]` reference pointing at the note.

**Fail-loud rule:** if `move` errors, or the CLI isn't installed at all,
_stop and ask Jonas_. Do not fall back to a filesystem move — that would
reintroduce the exact bug this routing exists to fix.

## Other commands — not currently used, kept for future hygiene

These aren't wired into any of the skill's current modes. Listed so you
don't have to go searching if the skill grows to need them. Verify with
`obsidian help <cmd>` before first use.

### `create` — new note (e.g. spinning up a project during Inbox processing)

```bash
obsidian create path="Projects/Foo" content="# Foo\n\n- [ ] First step"
obsidian create path="Projects/Foo" template="project-template"
```

Gotcha: **omit the `.md` extension** from `path=` — the CLI appends it.
Opposite convention from `move`/`read`/`append`.

### `search`, `search:context` — find tagged tasks across the vault

```bash
obsidian search query="#waiting-for" format=json
obsidian search:context query="#agenda" limit=20
```

- `format=json` returns a JSON array of file paths.
- Plain `search` gives file paths only; `search:context` returns matching
  lines with surrounding context.
- Alternative to Grep for Waiting-for / Agenda lookups, with the advantage
  of going through Obsidian's index rather than raw file contents.

### `tasks` — query tracked checkbox tasks

```bash
obsidian tasks                         # all tasks (complete + incomplete)
obsidian tasks done                    # only completed
obsidian tasks path="Projects/House.md"  # tasks in one file
obsidian tasks daily                   # tasks in today's daily note
```

In v1.12, plain `tasks` returns _all_ tasks (same as `tasks all`) —
filtering to incomplete-only needs post-processing (`grep "\[ \]"`).

### `backlinks`, `unresolved`, `orphans` — Weekly Review link hygiene

```bash
obsidian backlinks path="Projects/House.md"   # notes linking TO this note
obsidian unresolved                             # all unresolved [[wikilinks]]
obsidian orphans                                # notes with no links in or out
```

Candidate additions to Weekly Review step 3 if the vault starts
accumulating dangling refs.

### `eval` — escape hatch for anything the CLI doesn't expose

```bash
obsidian eval code="app.vault.getMarkdownFiles().length"
```

Runs arbitrary JS against the Obsidian API (`app`, `app.vault`,
`app.metadataCache`, etc.). Useful if you need something the Tasks plugin
exposes programmatically but the CLI doesn't surface — e.g. running a
Tasks-plugin query directly instead of parsing Dashboard.md.

Multiline JS fails inline; write to a temp file and use `code="$(cat file)"`.

## What NOT to use the CLI for in this skill

See the "What not to do" section in `SKILL.md` for the authoritative rule.
Short version: **capture and Inbox processing stay on the filesystem.** The
CLI auto-launches the desktop app and has non-trivial latency; neither is
acceptable in the zero-friction capture path or the tight inbox-processing
loop. The CLI is reserved for `move` (link preservation) and the
future-facing commands above.

## Source & trust

Command shapes here were verified against
[pablo-mano/Obsidian-CLI-skill](https://github.com/pablo-mano/Obsidian-CLI-skill)'s
command reference, which in turn wraps the official Obsidian 1.12 CLI.
That upstream repo has no LICENSE file, so this reference is re-written
from scratch in our own wording, trimmed to what obsidian-gtd uses, and
maintained independently. Authoritative source of truth for syntax
remains the CLI itself: `obsidian help <cmd>`.
