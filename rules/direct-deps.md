If a project directly invokes or imports a package, list it as a direct dep. Don't rely on a transitive/peer-resolved version brought in by another package.

If you `import x from 'foo'` or invoke `foo` as a CLI in a script or CI job, `foo` must be a direct dependency in the package.json of the workspace that uses it. Don't rely on it being installed transitively (e.g. as a peer dependency of some other package the project depends on).

## Why

Transitive/peer-resolved versions drift when the intermediate package changes its peer range. The version you tested against can silently shift, and there's no explicit pin to test future installs against.

Real example: a project removed `eslint` from root `package.json` after noticing `typescript-eslint` brings it in transitively as a peer. pnpm auto-installed a peer version and everything still worked. But the version was no longer pinned — if `typescript-eslint` later expanded its peer range or dropped a major, `pnpm install` would resolve to a different ESLint version without any code change in the repo. User pushed back: "what we interact with is still eslint itself, so it'd be right to keep it. Relying on a transitive dependency of something is not good practice."

## How to apply

- Anything invoked from a package.json script, CI command, or import statement in the workspace's source must be in that workspace's (or root's) `dependencies`/`devDependencies`.
- Peer-only installation is fine for *libraries* declaring what their *consumers* need to bring; it is not fine for *applications* declaring what they themselves use.
- When tempted to drop a dep because "it works without it via peer install", ask: do *we* call this package, or does only the intermediate use it? If we call it, pin it.
- This applies even when the package version turns out to be identical to the peer-resolved version — pinning is about the contract, not just the current resolved bytes.
