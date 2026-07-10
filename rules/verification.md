E2E/browser verification of code changes is **opt-in per project** — don't reach for it by default. Verify with the project's own test/lint/typecheck commands (CLAUDE.md checklist, package scripts). Run a dev-server + browser/screenshot verification loop only when the project's CLAUDE.md calls for it or the user asks. (Calibration: screenshot loops roughly doubled the token bill per delegated feature on a project whose preview and export already share one geometry source — the tests proved everything the pixels did.)

- This overrides the built-in `verify` skill's "exercise it end-to-end" default: treat that skill as something the user or project invokes, not a bar every change must clear.
- It applies to subagent briefs too — don't instruct delegated agents to boot dev servers and capture screenshots unless the project/user opted in.
- Project skills that *document how* to capture the app (dev hooks, headless render) are technique references, not mandates to do so on every change.
- When a change is visual/interactive by nature (layout, animation, drag behavior) and tests genuinely can't observe it, say so and ask before running the browser loop.
