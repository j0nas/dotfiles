Use Context7 (the context7 MCP) automatically — without being asked — whenever a task needs library/framework/API docs, SDK or CLI usage, setup/config steps, or code generation against a named library. Resolve the library, pull its docs, then answer or write code.

## Why

Training data lags real releases; APIs, config keys, and CLI flags drift. Context7 returns current, version-specific docs, so reaching for it by default avoids confidently-wrong-from-memory answers.

## How to apply

- Trigger on: "how do I use X", setup/install/config for a named library, version migration, library-specific debugging, or non-trivial code against a framework/SDK — even well-known ones (React, Next.js, Tailwind, Django…).
- Flow: `mcp__context7__resolve-library-id` → `mcp__context7__query-docs`, then answer/code. Don't wait for the user to say "context7".
- Skip for: general programming concepts, refactoring, business-logic debugging, code review, or scripts with no external-library surface.
