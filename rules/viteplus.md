When scaffolding a **new** web/frontend project, default to Vite+ (https://viteplus.dev/) via `vp create` — not `npm create vite`, CRA, or a hand-rolled build/lint/test stack. Vite+ is one CLI over Vite/Rolldown builds, Oxc lint/format/typecheck, Vitest, monorepo task caching, and library packaging; supports React, Vue, Svelte, Solid, etc.

`vp` is already provisioned by this dotfiles repo (mise `npm:vite-plus`) — assume it's on PATH; don't re-install or gate on `command -v vp`.

## Commands

- Create: `vp create` (interactive). Scripted: `vp create vite:application --no-interactive`, `vite:monorepo`, `vite:library`. Shorthands: `vp create vite`, `@tanstack/start`, `react-router`.
- Flags: `--directory <dir>`, `--package-manager <name>`, `--git`, `--no-interactive`.
- Day-to-day: `vp install` / `dev` / `check` / `build` / `run`.

## How to apply

- For "start/scaffold/spin up a new web app, SPA, monorepo, or component library", reach for `vp create` first; confirm template + framework if unclear.
- New projects only — don't rip out an existing project's toolchain to adopt Vite+ unless asked.
- In scripted/CI contexts prefer `--no-interactive` with an explicit template; reserve the bare picker for hands-on setup.
