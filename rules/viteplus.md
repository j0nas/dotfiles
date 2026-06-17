When scaffolding a **new** web/frontend project, default to Vite+ (https://viteplus.dev/) via `vp create` — not `npm create vite`, CRA, or a hand-rolled build/lint/test stack. Vite+ is one CLI over Vite/Rolldown build, Oxc lint/format/typecheck, Vitest, monorepo task caching, and library packaging. `vp` is already on PATH (mise `npm:vite-plus`) — don't re-install or gate on `command -v vp`.

## Defaults

- **Framework: React**, **styling: Tailwind** (`bradlc.vscode-tailwindcss` already wired). Don't ask — pick these unless an exception below clearly applies.
- Switch only when another choice is *far* better, not merely viable — surface the trade-off and confirm:
  - **Content-first sites** (marketing/blog/docs, mostly static) → **Astro** (zero JS by default; islands can still be React). Most common exception.
  - **Perf/bundle-critical or embeddable widgets** → **Svelte** or **Solid**.
  - **Framework-agnostic web components** → **Lit**.
  - **Existing ecosystem gravity** (Laravel+Inertia, or a Vue codebase) → **Vue**.
  - Mobile is *not* an exception — React Native stays in React.

## Commands

- Create: `vp create` (interactive); scripted `vp create vite:application|vite:monorepo|vite:library --no-interactive`; shorthands `vp create vite` / `@tanstack/start` / `react-router`.
- Flags: `--directory`, `--package-manager`, `--git`, `--no-interactive`. Day-to-day: `vp install` / `dev` / `check` / `build` / `run`.
- New projects only — don't rip out an existing toolchain unless asked. In CI prefer `--no-interactive` + explicit template.
