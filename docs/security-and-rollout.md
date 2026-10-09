# Security, telemetry, and rollout

Production serves Go templates and vanilla JavaScript from `web/`. The React directories remain archives; they are neither built by release CI nor embedded by the normal `embed` tag. `archive && embed` explicitly opts into the old frontend packages. Active browser tests run with `node --test web/tests/*.test.cjs`.

## Browser and public access

Browser mutations, including login, logout and initial setup, require a signed CSRF token. Fetch a browser page with a cookie jar, retain the `csrf_token` cookie, and send the page's `csrf-token` meta value as `X-CSRF-Token` for JSON requests or `csrf_token` in URL-encoded forms. Agent-token submissions are exempt. Tokens are signed with a process-local key; reload an open form after restarting the server.

Setup is a single conditional SQLite insert. The initial credential is written to `DATA_DIR/setup-token` with mode `0600`, printed nowhere, and removed after successful setup. The `/api/first-time-setup/restart` route is removed. Setup succeeds without restarting the server.

Public HTML, JSON, history, and browser WebSockets deny missing or empty public access rules. Empty checkbox selections remain empty. Membership changes validate the dashboard owner and every agent owner within the mutation transaction. Browser administrator snapshots contain only that user's agents. Nested disk capacity, read and write values are independently controlled.

Dashboard configuration versions increment through SQLite triggers on configuration and membership changes, including cascading agent removal. Permission resolution precedes every response-cache lookup. Public keys contain the immutable dashboard ID and current version; history also includes public agent ID, metric and resolved relative range. HTML keys include mount configuration. Builders revalidate the version before publishing. Identical requests coalesce within their versioned namespace. Mutations invalidate dynamic responses and close affected public sockets; socket handlers also revalidate their configuration/session periodically. Logout, ejection and password changes revoke session sockets. Password changes atomically update the hash and delete other sessions.

The response cache has a shared 128 MiB payload budget across dynamic namespaces, including the backing capacity of gzip and zstd variants. It performs expiration cleanup and least-recently-used eviction. Static assets have a separate immutable cache. Authenticated responses use `private, no-store`; dynamic public responses use `no-store`. These browser directives do not disable server caching. Responses negotiate accepted encodings, including quality values, and vary on `Accept-Encoding`.

Rate limits default to 1,200 requests/minute/IP with burst 120 and 60 uncached builds/minute/IP with burst 20. Coalesced followers consume no further build quota. IP state has a 10,000-entry ceiling, ten-minute idle eviction when full, and a shared overflow budget. At most 32 TSDB queries execute globally and 16 for public history; a shared 64-request waiting budget and two-second public deadline cover query waits and coalesced followers. Quotas return 429; saturated capacity returns 503, both with `Retry-After`. These are configurable starting limits, not measured capacity claims.

Forwarded headers are accepted only from `TRUSTED_PROXY_CIDRS`; the client chain is resolved from right to left through trusted hops. Untrusted forwarded headers are stripped before cookies or origins use them. Browser WebSockets default to same-origin and load additional explicit origins after URL configuration. Agent WebSockets authenticate with agent credentials. Browser messages are limited to 4 KiB; agent messages/submissions to 16 KiB. Browser connections are capped at 1,024 globally, 64 per user and 256 per dashboard. Writes have deadlines.

Executable handlers and bootstrap code live in external assets. JSON configuration is escaped and includes only module inputs. CSP restricts scripts to the current origin, framing is denied, and referrer/content-type protections apply. Webhooks allow HTTP(S), reject URL credentials, have a ten-second timeout, and follow at most three redirects without changing hosts. Administrator-configured private destinations remain supported.

## Telemetry and recovery

All protocols use the shared journaled ingestion path, serialized per agent with a bounded lock table. Source timestamps are preserved. Samples include `IntervalSeconds`; normalization prefers a supplied duration, then timestamp differences, then configured cadence. Network cumulative counters establish a missing baseline and treat resets as missing. Protocol parsers carry known absent readings rather than silently fabricating zero. Unknown disk capacity is represented as unavailable.

SQLite journals the normalized payload and lifetime-counter increment in one durable transaction. Recovery retries TSDB and metadata writes, never counter increments. Every TSDB append and commit is checked. Recovery runs before accepting traffic, and pending records for an agent are recovered before another batch for that agent. LTstats source timestamps identify samples; usable HetrixTools source times also identify samples. Sources without replay identifiers retain at-least-once delivery semantics: an external replay may add another observation. Completed journal identifiers are retained for deduplication; include the SQLite journal in storage-growth monitoring.

Existing byte series remain unchanged. History responses add `rate_data`, `interval_seconds`, and `legacy_estimate` per series. Rates aggregate interval bytes divided by observed sample durations; gauges aggregate separately. Older history without durations is estimated and marked, without rewriting TSDB data. Cache and TSDB history use the same source timestamps and rate calculation. Alerts use interval-weighted throughput, reject missing/stale or insufficient coverage, and do not resolve unavailable telemetry as healthy. Heartbeat offline status remains three minutes; browser feed staleness is 45 seconds.

Lifetime totals count observed traffic since agent creation. Collection gaps are excluded, and recreating the agent resets totals. These are not estimates of all traffic generated during outages.

## Deployment sequence

Release the security/privacy, metric correctness, UX, and deployment changes as separate reviewed stages. Before deploying any stage that changes storage, stop ingestion and back up the entire data directory, including SQLite/WAL and TSDB. Test recovery and migration on a copy. SQLite migrations are additive and idempotent and fail startup on unexpected errors.

The runtime is pinned to Alpine 3.22.1 and runs as UID/GID 10001. Existing bind mounts must be writable by that account (`chown -R 10001:10001 ./data` on the intended data directory). Compose retains `restart: unless-stopped`; HTTP health checks are removed and no health route is added. Shutdown stops HTTP traffic, cancels background work, closes browser/agent sockets, waits for the central routine, and closes storage.

Retention remains unlimited unless configured. `RETENTION_DURATION` uses Go duration syntax, such as `2160h`; `STORAGE_MAX_BYTES` sets the TSDB block-size ceiling. These settings do not impose a whole-volume quota on SQLite, WAL, head blocks or journals.

Maintenance logs include response-cache hits/misses, bytes, evictions, active/coalesced builds, waiting requests, quota rejections, overflow usage, and journal backlog. Ingestion failures and restarts are logged. Collect process/volume monitoring for restarts and storage growth.

Before publishing, run Go tests, targeted race tests, active JavaScript tests, embedded builds, and browser checks at 320–1440 px in both themes. Run representative workload/load tests against production-like storage and agent volumes before tuning the defaults. Synthetic quota/cache tests verify behavior, not production capacity. Preserve the backup until migrations, replay, visibility revocation, session handling and storage growth are confirmed after release.
