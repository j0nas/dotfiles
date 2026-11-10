# Read-only recipes

These are starting points by type of system. Each one gives the preferred method, what it yields for the report, and the trap. Confirm current commands with Context7 or the vendor's docs before use: CLIs and APIs drift, and a mistyped subcommand can write. Record the method you actually used in `access.md`.

Order of preference for any system:
1. a read-only token or role created for this;
2. a read-only transaction, branch or replica;
3. GET-only API calls with an existing credential;
4. reading page text in your own browser tab.

## Code and delivery

- **GitHub:** `gh api` GET endpoints only, or `gh pr list --json` / `gh run list --json` with a fine-grained token scoped to read.
  - Yields: PR size, time to merge, review latency, CI duration and failure rate, branch protection (whether checks are required), deploys and environments.
  - Trap: `gh` subcommands that comment, label or re-run.
- **GitLab, Bitbucket, Azure DevOps:** the same data, from their REST APIs with a read-scoped token.

## Hosting and cloud

- **Vercel and Netlify:** dashboard text, or the REST API with GET requests.
  - Yields: regions, environments and which secrets are scoped to which, deploy frequency and failures, firewall and bot protection, log retention, function regions compared with database region.
  - Trap: `vercel env pull` writes secrets to disk. Never run it.
- **AWS:** a ReadOnlyAccess or ViewOnlyAccess role, with `describe-*`, `list-*` and `get-*` calls.
  - Yields: regions, IAM shape, public buckets, alarms, cost explorer.
  - Trap: `get-secret-value` returns secrets; skip it.
- **GCP:** Viewer role, `gcloud ... list/describe`.
- **Azure:** Reader role, `az ... list/show`.
- **Railway, Render, Fly, Heroku:** dashboard text or CLI status and list commands.
  - Yields: instance count, restart policy, health checks, region.
  - Trap: commands that deploy or restart.

## Data

- **Postgres** (Neon, RDS, Supabase, Cloud SQL):
  - Connect with a read-only role when possible, and always inside `BEGIN READ ONLY`.
  - Prefer a branch or replica. Neon branches are cheap to create, but creating one is a write to the project; ask first.
  - Yields: table sizes and row counts (`pg_stat_user_tables`), index use, slow queries (`pg_stat_statements` if enabled), connection limits, settings.
  - Trap: queries that aren't cheap on a production primary.
- **MySQL:** a read-only user, `SET SESSION TRANSACTION READ ONLY`.
- **MongoDB:** the `read` role, `find` and `aggregate` with `$limit`.
- **Redis:** `INFO` and `SCAN` with small counts, never `KEYS *` in production.

## Observability and product

- **Sentry:** an API token with read scopes.
  - Yields: top issues by events and users, error rate trend, release health, which projects exist, alert rules.
- **Datadog, Grafana, New Relic, Honeycomb:** read-only API keys or viewer role.
  - Yields: latency and error service-level indicators, monitors and alerts, dashboards that exist.
- **PostHog, Amplitude, Mixpanel:** a personal API key with read scope, or viewer access.
  - Yields: active users, peak concurrency, funnels the team watches, feature flags in use.
- **Vercel Analytics and Speed Insights:** dashboard text.
  - Yields: visitors, real-user speed per route and device.

## Work tracking

- **Jira, Linear, GitHub Issues:** read-only API.
  - Yields: open bug count and age, how specs are written (useful for task discovery in factory readiness), incident tickets.

## Billing (only if the user wants cost)

Read invoices and upcoming-invoice pages, not usage dashboards. Subtract the plan's included credit. Label each line as plan fee, seat, add-on or usage. Marketplace add-ons (databases sold through the host) often bill separately. Say which billing cycle each number belongs to.
