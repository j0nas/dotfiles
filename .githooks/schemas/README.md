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

To refresh a schema: `curl -fsSL <upstream> -o .githooks/schemas/<file>` and commit.
