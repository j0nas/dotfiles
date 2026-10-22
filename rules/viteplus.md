When scaffolding a **new** web/frontend project, default to Vite+ (https://viteplus.dev/) — not `npm create vite`, CRA, or a hand-rolled build/lint/test stack. Vite+ is one CLI (`vp`) over Vite/Rolldown build, Oxc lint/format/typecheck, Vitest, monorepo task caching, and library packaging. There is no global `vp` on PATH, on purpose: each project runs the `vite-plus` pinned in its own lockfile, and scaffolding fetches the latest release with `pnpm dlx`.

## Defaults

- **Framework: React**, **styling: Tailwind** (`bradlc.vscode-tailwindcss` already wired). Don't ask — pick these unless an exception below clearly applies.
- Switch only when another choice is *far* better, not merely viable — surface the trade-off and confirm:
  - **Content-first sites** (marketing/blog/docs, mostly static) → **Astro** (zero JS by default; islands can still be React). Most common exception.
  - **Perf/bundle-critical or embeddable widgets** → **Svelte** or **Solid**.
  - **Framework-agnostic web components** → **Lit**.
  - **Existing ecosystem gravity** (Laravel+Inertia, or a Vue codebase) → **Vue**.
  - Mobile is *not* an exception — React Native stays in React.

## Commands

- Create: `pnpm --package=vite-plus dlx vp create` (interactive); scripted `pnpm --package=vite-plus dlx vp create vite:application|vite:monorepo|vite:library --no-interactive`; shorthands `vite` / `@tanstack/start` / `react-router` in place of the template. `--package=vite-plus` is required: the package ships two binaries (`vp`, `vpr`).
- Flags: `--directory`, `--package-manager`, `--git`, `--no-interactive`.
- Inside a project: `pnpm install`, then `pnpm vp check` / `test` / `build` / `dev` / `run`, or the package scripts. A bare `vp` is "command not found" outside a git hook (Vite+'s hook dispatcher adds `node_modules/.bin` to PATH).
- New projects only — don't rip out an existing toolchain unless asked. In CI prefer `--no-interactive` + explicit template.
