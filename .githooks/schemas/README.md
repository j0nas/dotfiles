# Vendored config schemas

Pinned, offline copies of the JSON Schemas the pre-commit hook validates
against. Vendored (not fetched at commit time) so validation is deterministic
and works offline — same principle as pinning a dependency rather than tracking
a moving remote.

| File | Validates | Upstream (re-download to refresh) |
|------|-----------|-----------------------------------|
| `claude-code-settings.json` | `dot_claude/settings.json` | https://www.schemastore.org/claude-code-settings.json |
| `starship.json` | `dot_config/starship.toml` | https://starship.rs/config-schema.json |
| `mise.json` | `dot_config/mise/config.toml` | https://mise.jdx.dev/schema/mise.json |
| `opencode.json` | (unused — see note) | https://opencode.ai/config.json |

**opencode.json is vendored but not enforced.** Its schema pins a closed enum of
model names that rejects local/custom models (e.g. an LM Studio model), so the
hook would false-positive on a valid config. `opencode.json` is checked for JSON
syntax only; the inline `$schema` still drives editor hints.

To refresh a schema: `curl -fsSL <upstream> -o .githooks/schemas/<file>` and commit.
