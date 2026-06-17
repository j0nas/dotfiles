When scaffolding a **new** web/frontend project, default to Vite+ (https://viteplus.dev/) via `vp create` — not `npm create vite`, CRA, or a hand-rolled build/lint/test stack. Vite+ is one CLI over Vite/Rolldown builds, Oxc lint/format/typecheck, Vitest, monorepo task caching, and library packaging; supports React, Vue, Svelte, Solid, etc.

`vp` is already provisioned by this dotfiles repo (mise `npm:vite-plus`) — assume it's on PATH; don't re-install or gate on `command -v vp`.

## Framework & styling defaults

- **Framework: React.** Default to the React template (`vp create vite` → React, or a React shorthand like `@tanstack/start` / `react-router`). Don't ask which framework — pick React unless one of the exceptions below clearly applies.
- **Styling: Tailwind CSS.** Default to Tailwind for styling; it's already the IDE story here (`bradlc.vscode-tailwindcss`). Wire it into the scaffold unless the user asks for something else.

### When to pick something other than React

These are the cases where another choice is *far* more appropriate — not merely viable. Surface the trade-off and confirm rather than defaulting:

- **Content-first sites** (marketing, blog, docs, mostly-static with light interactivity) → **Astro**. Ships zero JS by default; islands hydrate only the interactive bits (and those islands can still be React). This is the most common exception.
- **Bundle-size / runtime-perf critical, or embeddable widgets** dropped into pages you don't control → **Svelte** (compiles the framework away) or **Solid** (fine-grained reactivity, no VDOM).
- **Framework-agnostic web components / design-system primitives** meant to work in any host or none → **Lit** (or Svelte custom elements).
- **Strong existing ecosystem gravity** (e.g. Laravel + Inertia, or a codebase already on Vue) → **Vue**. Fighting the ecosystem costs more than the framework choice saves.

Mobile is *not* an exception — React Native keeps you in React. Vite+ supports React/Vue/Svelte/Solid and Astro runs on Vite too, so none of these leave the toolchain.

## Commands

- Create: `vp create` (interactive). Scripted: `vp create vite:application --no-interactive`, `vite:monorepo`, `vite:library`. Shorthands: `vp create vite`, `@tanstack/start`, `react-router`.
- Flags: `--directory <dir>`, `--package-manager <name>`, `--git`, `--no-interactive`.
- Day-to-day: `vp install` / `dev` / `check` / `build` / `run`.

## How to apply

- For "start/scaffold/spin up a new web app, SPA, monorepo, or component library", reach for `vp create` first with the React + Tailwind defaults above; only stop to confirm the framework when an exception case (content-first site, embeddable widget, etc.) might apply.
- New projects only — don't rip out an existing project's toolchain to adopt Vite+ unless asked.
- In scripted/CI contexts prefer `--no-interactive` with an explicit template; reserve the bare picker for hands-on setup.
