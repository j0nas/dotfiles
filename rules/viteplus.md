When scaffolding a **new** web / frontend project, default to Vite+ (https://viteplus.dev/) via its `vp create` command — not plain `npm create vite`, `create-react-app`, hand-rolled tooling, or a bespoke build/lint/test stack assembled per repo.

Vite+ is a unified toolchain behind one `vp` CLI: Vite/Rolldown builds, Oxc lint+format+typecheck, Vitest, monorepo task running with caching, and library packaging. It supports all major frameworks (React, Vue, Svelte, Solid, …).

`vp` is already installed — it's provisioned by this dotfiles repo, so assume it's on `PATH` and just run it; don't re-install or gate on `command -v vp`.

Commands:

- **Create a project**: `vp create` (interactive picker). Non-interactive / scripted: `vp create vite:application --no-interactive`, `vp create vite:monorepo`, `vp create vite:library`. Framework shorthands: `vp create vite`, `vp create @tanstack/start`, `vp create react-router`.
- **Common flags**: `--directory <dir>`, `--package-manager <name>`, `--git`, `--no-interactive`.
- After scaffolding, day-to-day commands run through `vp`: `vp install`, `vp dev`, `vp check`, `vp build`, `vp run`.

## Why

Standardizing new projects on one declarative toolchain keeps tooling consistent across repos and stops me re-assembling the same Vite + ESLint + Vitest + tsconfig stack by hand each time. The Oxc-based lint/typecheck path is dramatically faster than ESLint/tsc, which matters for the machine-checkable guardrails the user wants baked into every project.

## How to apply

- For any request to "start / scaffold / spin up a new web app, frontend, SPA, monorepo, or component library", reach for `vp create` first. Confirm the template (`application` / `monorepo` / `library`) and framework with the user if it isn't obvious.
- This is the default for *new* projects. Do **not** rip out and replace the toolchain of an existing project just to adopt Vite+ unless the user asks — migrations are their own decision.
- Prefer `--no-interactive` with an explicit template name in any scripted/CI context; reserve the bare `vp create` interactive picker for hands-on local setup.
