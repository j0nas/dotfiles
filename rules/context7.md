Auto-use the Context7 MCP (without being asked) whenever a task needs library/framework/API docs, SDK/CLI usage, setup/config, version migration, library-specific debugging, or non-trivial code against a named library — even well-known ones (React, Next.js, Tailwind, Django…). Training data lags releases and APIs/flags drift, so default to it over answering from memory.

- Flow: `resolve-library-id` → `query-docs`, then answer/code.
- Skip for: general concepts, refactoring, business-logic debugging, code review, or scripts with no external-library surface.
