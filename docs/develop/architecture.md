# Architecture

This page explains how the CertainStats server is put together: its parts, how data flows through it, how it stores and serves data, and where the code lives. It is for developers. For running a server, start at the [docs index](../README.md).

## Overview

CertainStats is a self-hosted, single-binary monitoring server. Agents on remote machines send system metrics (CPU, memory, disk, network). The server stores them and shows them in two web interfaces:

| Side | For | Default path |
| --- | --- | --- |
| **Admin panel** | The owner: agents, alerts, dashboards, collectors, settings | `/` |
| **Public dashboards** | Anyone with the link: a chosen subset of agent data | `/dashboard` |

Both are rendered on the server with Go `html/template` and made live with small vanilla JavaScript modules; see [frontend](frontend.md). Where each side is served is configured with [URL or path mode](../operate/configuration.md#where-the-panel-and-dashboards-live).

Main building blocks:

| Part | Used for |
| --- | --- |
| [chi v5](https://github.com/go-chi/chi) | HTTP router |
| Prometheus TSDB | Time series: agent metrics (`tsdb/`) and collector metrics (`collector_tsdb/`) |
| SQLite (`modernc.org/sqlite`) | Accounts, sessions, agents, dashboards, alerts, collectors, the ingestion journal (`agent_state.db`); CrowdSec (`crowdsec.db`) and Knot DNS (`knot.db`) data |
| WebSockets | Live pushes to browsers, Beszel agents, collectors |

## Data flow

```
 LTstats agent ── POST /submit (packed binary) ──┐
 HetrixTools agent ── POST /submit (gzip JSON) ──┤
 Beszel agent ── WS /api/beszel/agent-connect ───┤
                                                 ▼
                            agent_parser: detect format, normalise
                                                 ▼
                     SQLite ingestion journal (durable, per agent)
                                                 ▼
               TSDB samples  +  SQLite metadata and lifetime counters
                                                 ▼
               realtime cache ── /api/ws (owner), /api/public/ws/{id} (public)

 Collector ── WS /api/collectors/connect ── collector type ── collector_tsdb, crowdsec.db, knot.db
```

1. **Agents send data.** LTstats and HetrixTools agents push to `POST /submit`; Beszel agents keep a WebSocket open and the server pulls from them.
2. **Parsing.** The `agent_parser` registry detects the agent format and turns it into normalised samples.
3. **Journal, then storage.** Each batch is written to the SQLite ingestion journal first, then to the TSDB and SQLite metadata. See [ingestion and recovery](#ingestion-and-recovery).
4. **Live push.** The realtime cache feeds the owner's `/api/ws` connection and public dashboard connections (`/api/public/ws/{id}`), every ten seconds. See [browser protocol](browser-protocol.md).
5. **Charts** read history through `GET /api/metrics`.
6. **Collectors** use their own connection and storage. See [collector internals](collectors/README.md).
7. **Network monitors** run on Beszel agents and arrive with their telemetry. See [network monitoring internals](network-monitoring.md).

## Agent drivers

| Driver | Transport | Format | Auth |
| --- | --- | --- | --- |
| `beszel` | WebSocket `GET /api/beszel/agent-connect`; the server pulls telemetry | CBOR | Token in `X-Token` header, plus SSH key |
| `ltstats` | `POST /submit` | Custom packed binary (`NetHeader`, `Details`, `StatT`) | Token in the payload |
| `hetrixtools` | `POST /submit` | Gzip + Base64 JSON | Token in the payload |

Each agent has a unique `agent_id` (`agt_…`), a `nickname`, and a hardware snapshot: CPU model and cores, RAM and swap size, disks with mount points and partitions, OS version and uptime. Lifetime network and disk byte counters are kept separately from rates. Wire formats, provisioning and the agent API are in [agent internals](agents.md).

## Ingestion and recovery

- **One path for all protocols.** Every protocol goes through the shared journaled ingestion path, one batch at a time per agent, using 256 striped per-agent locks.
- **Timestamps.** The source's timestamps are kept.
- **Sample durations.** Samples carry `IntervalSeconds`. Normalisation takes a supplied duration first, then the gap between timestamps, then the configured cadence.
- **Missing readings stay missing.** Cumulative network counters set a baseline when one is missing and treat resets as missing data. Parsers carry known-absent readings instead of inventing zeros. Unknown disk capacity is unavailable, not zero.
- **The journal.** SQLite stores the normalised payload and the lifetime-counter increment in one durable transaction.
  - Recovery retries the TSDB and metadata writes, never the counter increment.
  - Every TSDB append and commit is checked.
  - Recovery runs before the server accepts traffic, and an agent's pending records are recovered before its next batch.
- **Duplicates.** LTstats source timestamps identify samples, and so do usable HetrixTools source times. Sources without replay identifiers are at-least-once: an external replay may add another observation.
- **Journal cleanup.** Completed entries drop their payload and keep the identifier plus an 8-byte completion time in the `payload` column, so resent batches are still recognised. The hourly cleanup deletes completed entries older than `INGESTION_JOURNAL_RETENTION` and returns the space with SQLite incremental vacuum. Pending entries are never deleted.

### Rates and history

- History responses carry `rate_data`, `interval_seconds` and `legacy_estimate` per series.
- Rates sum interval bytes and divide by the observed sample durations; gauges are aggregated separately.
- Older history without durations is estimated and marked `legacy_estimate`, without rewriting stored data.
- The realtime cache and TSDB history use the same source timestamps and rate calculation.
- Alerts use interval-weighted throughput, reject missing, stale or under-covered data, and never resolve on unavailable telemetry.
- An agent is offline after three minutes without a heartbeat. A browser feed is stale after 45 seconds.
- Lifetime totals count observed traffic since agent creation. Gaps are excluded, and recreating the agent resets totals.

## Metrics

| Metric | Description | Unit |
| --- | --- | --- |
| `agent_cpu_usage` | CPU utilisation | % |
| `agent_cpu_iowait` | CPU I/O wait | % |
| `agent_cpu_steal` | CPU steal | % |
| `agent_ram_used` | RAM used | bytes |
| `agent_swap_used` | Swap used | bytes |
| `agent_disk_used`, `agent_disk_usage` | Disk usage | bytes, % |
| `agent_disk_read_bytes` | Disk read | bytes in the interval |
| `agent_disk_write_bytes` | Disk write | bytes in the interval |
| `agent_rx_bytes` | Network received | bytes in the interval |
| `agent_tx_bytes` | Network sent | bytes in the interval |
| `agent_sample_interval_seconds` | Sample duration; rates are interval bytes divided by this, at read time | s |

- **Query:** `GET /api/metrics?metric=<key>&agent_id=<id>&hours=<n>`, or `start` and `end` in Unix ms instead of `hours`. One metric and one agent per request; GET parameters never carry comma-separated lists.
- **Range:** at most two years (`internal/metrics/timerange.go`), ending no later than now. Responses return at most about 1,000 points.
- **Response:** a list of series, each with `[timestamp_ms, value]` points and a `labels` map that tells disks and network interfaces apart.

### TSDB options

Both TSDBs are opened with `internal/tsdbopts`, which sets the maximum block duration the way the Prometheus server does: a tenth of retention, at most 31 days, at most 7 days when a size limit is set, and never below 2 hours. The library default (`tsdb.DefaultOptions()`, 2-hour minimum and maximum) never merges blocks. See the [case study](../case-study/tsdb-block-growth.md).

## Alerts

### Conditions

| Trigger | Fires when |
| --- | --- |
| `agent_down` | The agent goes offline (no heartbeat) |
| `cpu_usage`, `cpu_iowait`, `cpu_steal` | CPU %, I/O wait % or steal % crosses the threshold for the duration |
| `ram_usage`, `swap_usage`, `disk_usage` | RAM, swap or disk % crosses the threshold |
| `net_rx`, `net_tx` | Network throughput (KB/s) crosses the threshold |
| `disk_read`, `disk_write` | Disk I/O (KB/s) crosses the threshold |
| `network_loss` | A network monitor's loss over one hour is above the threshold; see [network monitoring](network-monitoring.md#alert-evaluation) |
| `knot_*`, `crowdsec_*` | Collector conditions; see [Knot DNS](../operate/collectors/knot.md#alert-rules) and [CrowdSec](../operate/collectors/crowdsec.md#alert-rules) |

### Notification actions

| Type | What it does |
| --- | --- |
| `webhook` | HTTP POST with a configurable JSON payload |
| `discord` | Discord webhook with a pre-formatted embed |
| `preset` | Uses a saved alert target by ID |

Webhook safety rules are in [security](../operate/security.md#webhooks). The operator view, with templates and default payloads, is in [alerts](../operate/alerts.md).

### Lifecycle

```
IDLE → FIRING (threshold breached for the duration) → notification: sent / failed
FIRING → RESOLVED (metric back to normal) → resolve notification
FAILED → retry (manual or automatic)
```

- Incident history is stored in SQLite (`alert_history`, `alert_history_events`): trigger and resolve times, breach value, notification status and errors.
- What a rule watches is stored by kind: agents (`alert_agents`), network monitors (`alert_monitors`), collector subjects (`alert_subjects`).
- Notification delivery and retries run in `internal/alertdelivery`.

## Public dashboards

- **Access rules.** Each dashboard has an access rule listing which metric fields are visible. It is checked before any data is returned. Missing or empty rules deny everything.
- **Aliases.** Public agents are shown by a `public_id` and a per-dashboard alias, hiding the internal `agent_id`.
- **Live feed.** `GET /api/public/ws/{id}` pushes agent snapshots filtered by the access rule.
- **Access rule shape.** `{"public": {"allowed_fields": [...], "allowed_metrics": [...], "max_days": N}}`, validated by `internal/dashboard/accessrules`: fields and metrics must come from fixed lists, and `max_days` must be 1, 2, 7, 30, 90, 180, 365 or 730. Operator view: [status pages](../operate/dashboards.md).
- **Ownership.** Membership changes validate the dashboard owner and every agent owner inside the same transaction.

## Sessions and sign-in

- The JSON API (`/api/auth/login`) and the web UI (`/login`) share one session system.
- Session tokens are 32 random bytes, URL-safe base64 (`auth.GenerateSessionToken()`).
- Sessions last 24 hours, or 30 days with "remember me".
- The cookie is `session_token`: `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure` over direct HTTPS or behind a trusted proxy that reports HTTPS. Its expiry matches the database `expires_at`.
- Sessions record `user_id`, `ip_address`, `user_agent`, `created_at`, `last_connected_at` and `expires_at`. Users can review them and eject one or all others from `/settings`.
- Every authenticated request is checked (`requireAuth` for the API, `RequireAuthWeb` for pages). Expired sessions are purged hourly by the background routine.
- First-time setup is a single conditional SQLite insert, so only one first account can ever be created.

## Caching and request handling

- **Dashboard versions.** A dashboard's configuration version increments through SQLite triggers on configuration and membership changes, including cascading agent removal.
- **Permissions first.** Permission resolution happens before every response-cache lookup.
- **Cache keys.**
  - Public keys contain the immutable dashboard ID and its current version.
  - History keys add the public agent ID, the metric and the resolved relative range.
  - HTML keys include the mount configuration.
- **Builds.** A builder re-checks the version before publishing its result. Identical requests coalesce within their versioned namespace.
- **Invalidation.** Changes invalidate dynamic responses and close affected public sockets. Socket handlers also re-check their configuration and session regularly.
- **Response cache.** One 128 MiB payload budget across all dynamic namespaces, including the gzip and zstd variants. It expires entries and evicts the least recently used. Static assets have a separate, immutable cache.
- **Encoding.** Responses negotiate the accepted encodings, including quality values, and vary on `Accept-Encoding`.

Rules for writing cached handlers are in [caching standards](caching-standards.md). Rate limits and capacity limits are in [security](../operate/security.md#limits).

## Assets and page code

- Executable code lives only in external asset files. Each page script reads its own escaped `#page-config` JSON, which holds only that module's inputs.
- Assets are split into areas by audience (`shared`, `admin`, `public`, `auth`), and each router serves only its areas. See [frontend JavaScript build](frontend-js-build.md).

## Shutdown

Shutdown stops HTTP traffic, cancels background work, closes browser and agent connections, waits for the background routine, and closes storage.

## Code layout

Executables live in `cmd/`: `certainstats` (the server), `certainstats-collector`, and one-off maintenance tools such as `cleanup-beszel-io`. Everything else is under `internal/`:

| Package | Responsibility |
| --- | --- |
| `agent` | Agent provisioning, submissions, Beszel connections, ingestion |
| `agent_data` | Agent sample types and token parsing |
| `agent_parser` | Format detection and parsers for Beszel, LTstats, HetrixTools |
| `agentmeta` | Agent runtime metadata and capabilities |
| `alert` | Alert rules and evaluation |
| `alertdelivery` | Notification delivery and retries |
| `auth` | Sessions, sign-in, first-time setup |
| `base` | Shared domain types: agents, alerts, dashboards, metric points, API response shapes |
| `collector` | Collector server side; `collector/type/*` per type, `collector/tsdb`, `collector/wire` |
| `collectoragent` | The collector binary's logic, with `type/*` modules |
| `compress` | zstd and gzip pre-compression |
| `context` | Response caches, cache keys, build coalescing |
| `dashboard` | Dashboards and public access rules |
| `lifecycle` | Shutdown and restart requests |
| `logger` | Logging |
| `metrics` | Realtime cache, TSDB queries, time ranges |
| `minify` | Asset minification and SRI hashes |
| `networkmonitor`, `networkservice` | Network monitor model and adapters; the service that syncs and ingests them |
| `notifications` | Notification dispatch and webhook sending |
| `response` | JSON response helpers |
| `routine` | The background routine: sweeps and hourly maintenance |
| `security` | Rate limits, proxies, CSRF, origin checks |
| `store` | Storage interfaces; `store/sqlite` implements them |
| `tsdbopts` | Shared TSDB options |
| `web` | Server-rendered pages and the in-place router |
| `ws` | Browser WebSockets and the broadcaster |

### Main database tables

`agent_state.db` holds: `users`, `sessions`, `agents`, `beszel_ssh`, `agent_disk_odometers`, `dashboards`, `dashboard_agents`, `alerts`, `alert_agents`, `alert_monitors`, `alert_subjects`, `alert_crowdsec_subjects` (moved to `alert_subjects` on upgrade), `alert_targets`, `alert_history`, `alert_history_events`, `collectors`, `collector_types`, `ingestion_journal`, `network_monitors`, `network_monitor_latest`, `network_monitor_ingestion`, `network_monitor_sync`.

Migrations live in `internal/store/sqlite`. They only add things, can run more than once, and stop startup on an unexpected error.
