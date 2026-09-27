# Vendored config schemas

Pinned, offline copies of the JSON Schemas the pre-commit hook validates
against. Vendored (not fetched at commit time) so validation is deterministic
and works offline — same principle as pinning a dependency rather than tracking
a moving remote.

| File | Validates | Upstream (re-download to refresh) |
|------|-----------|-----------------------------------|
| `claude-code-settings.json` | `Claude-settings.json` + the merged `dot_claude/modify_settings.json` output | https://www.schemastore.org/claude-code-settings.json |
| `starship.json` | `dot_config/starship.toml` | https://starship.rs/config-schema.json |
| `mise.json` | `dot_config/mise/config.toml` | https://mise.jdx.dev/schema/mise.json |

**SchemaStore lags Claude Code.** The Claude schema allows unknown keys, so
`validate` also requires every top-level key in `Claude-settings.json` to be in
the schema or in its `claude_unschemaed` allowlist (real settings SchemaStore
doesn't list yet). After a refresh, `validate` fails on any allowlisted key the
schema has since learned — drop it from the list.

To refresh a schema: `curl -fsSL <upstream> -o .githooks/schemas/<file>` and commit.
