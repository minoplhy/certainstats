# Configuration

The CertainStats server reads its settings from environment variables. This page lists every server setting. Collectors run on other machines and have their own variables: see [collectors](collectors/README.md#collector-settings), [Knot DNS](collectors/knot.md#setup) and [CrowdSec](collectors/crowdsec.md#setup).

The server reads only its process environment. Docker Compose forwards values from `.env` to that environment; the application does not read `.env` itself. Most settings are read once at startup, so restart the server after changing them.

## Where the panel and dashboards live

CertainStats has two sides: the **admin panel**, where you sign in and manage things, and **public dashboards**, the status pages anyone can open. Choose one of two ways to place them. Mixing the two stops startup.

- **URL mode:** set `PANEL_URL`, and optionally `PUBLIC_URL`.
  - Each side is served only on the host named in its URL, for browsers and agents alike. Any other host gets 404.
  - Without `PUBLIC_URL`, dashboards are served at `/dashboard` on the panel's host.
- **Path mode:** set no URL, and optionally `PANEL_PATH` and `PUBLIC_PATH`. Both sides answer on any host.

These combinations are errors:
- `PUBLIC_URL` without `PANEL_URL`.
- A `*_PATH` other than its default alongside a URL.

A `*_PATH` equal to its default (`/` or `/dashboard`) next to a URL is ignored with a warning, so older compose files keep working.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PANEL_URL` | *none* | URL mode: admin panel URL including its host, e.g. `https://admin.example.com` or `https://example.com/admin`. Agent and collector install steps use its scheme and host, so they say `https` behind a reverse proxy even when `TRUSTED_PROXY_CIDRS` is not set. |
| `PUBLIC_URL` | `<PANEL_URL host>/dashboard` | URL mode: public dashboards URL including its host. Requires `PANEL_URL`. |
| `PANEL_PATH` | `/` | Path mode only: subpath for the admin panel. |
| `PUBLIC_PATH` | `/dashboard` | Path mode only: subpath for the public dashboards. |

## Server basics

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | TCP port the server listens on. |
| `HOST` | `0.0.0.0` | Host or IP address the server binds to. |
| `DATA_DIR` | `./data` | Directory for all stored data: the SQLite databases and both TSDBs. See [what to back up](install-and-upgrade.md#back-up). |
| `UPDATE_EVERY` | `60` | Metric sweep frequency, in seconds. |
| `BESZEL_EVERY` | `60` | Beszel agent metrics sweep frequency, in seconds. Follows `UPDATE_EVERY` by default. |
| `DEBUG` | `false` | Verbose trace logging. |

## Proxies, origins and rate limits

These settings control who the server trusts and how much one IP address may ask for. See [security](security.md) for how they fit together.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `TRUSTED_PROXY_CIDRS` | empty | Comma-separated CIDRs of trusted reverse proxies. Forwarded headers (`X-Forwarded-For`, `X-Forwarded-Host`, forwarded HTTPS) from any other peer are ignored. |
| `ALLOWED_ORIGINS` | *none* | Comma-separated HTTP(S) origins allowed to open browser WebSockets, on top of same-origin. Empty means same-origin only. |
| `PUBLIC_REQUESTS_PER_MINUTE` | `1200` | Per-IP refill rate for anonymous requests. |
| `PUBLIC_REQUEST_BURST` | `120` | Per-IP burst for anonymous requests. |
| `PUBLIC_BUILDS_PER_MINUTE` | `60` | Per-IP refill rate for uncached builds: requests whose answer is not in the cache and must be computed. |
| `PUBLIC_BUILD_BURST` | `20` | Per-IP burst for uncached builds. Identical requests that arrive while a build runs wait for it and share its result, at no extra build cost. |
| `LOGIN_ATTEMPTS_PER_MINUTE` | `10` | Per-IP refill rate for sign-in and first-time setup attempts. |
| `LOGIN_ATTEMPT_BURST` | `10` | Per-IP burst for sign-in and setup attempts; further attempts get 429. |

## Retention and storage

Without these settings, the agent TSDB keeps data forever. Other stores have their own defaults, listed below. None of these is a whole-volume quota: SQLite, its WAL, TSDB head blocks and journals are not counted.

**About block merging.** Both TSDBs store data in blocks that start at 2 hours and are merged into larger ones over time. Data is deleted a whole block at a time, which is why the limits below can overshoot a little. The [TSDB case study](../case-study/tsdb-block-growth.md) explains why merging matters.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `RETENTION_DURATION` | `0` (unlimited) | Agent TSDB retention in Go duration syntax, e.g. `2160h`. Blocks merge up to a tenth of retention (31 days when unlimited), so data can stay up to about 10% past the limit before its block is deleted. |
| `STORAGE_MAX_BYTES` | `0` (unlimited) | Agent TSDB size limit, in bytes. When set, blocks merge up to 7 days at most, so reaching the limit deletes about a week of the oldest history at a time. |
| `COLLECTOR_RETENTION_DURATION` | `2160h` (90 days) | Retention of the collectors' own TSDB, `collector_tsdb`, separate from the agent TSDB. `0` keeps data forever. Knot DNS counters live here. Blocks merge up to a tenth of retention (31 days when unlimited). |
| `COLLECTOR_STORAGE_MAX_BYTES` | `0` (unlimited) | Size limit of `collector_tsdb`, in bytes, independent of `STORAGE_MAX_BYTES`. When set, blocks merge up to 7 days at most. |
| `CROWDSEC_RAW_RETENTION` | `0` (unlimited) | How long CrowdSec alerts and decisions are kept in `crowdsec.db`, e.g. `4320h`. At least `1h`. See [CrowdSec](collectors/crowdsec.md#retention). |
| `CROWDSEC_ROLLUP_RETENTION` | `0` (unlimited) | How long CrowdSec hourly counts are kept. At least `1h`. |
| `KNOT_DNSTAP_RAW_RETENTION` | `168h` (7 days) | How long Knot DNS dnstap minute buckets are kept in `knot.db`. `0` keeps them forever. At least `1h`. See [Knot DNS](collectors/knot.md#storage-and-retention). |
| `KNOT_DNSTAP_ROLLUP_RETENTION` | `2160h` (90 days) | How long Knot DNS dnstap hourly buckets are kept. `0` keeps them forever. At least `1h`. |
| `INGESTION_JOURNAL_RETENTION` | `24h` | How long completed agent ingestion batches stay in the main database, so a resent batch is recognised and skipped. Pending batches are always kept. `0` keeps them forever. At least `1h`. The hourly cleanup returns freed space to the filesystem (SQLite incremental vacuum). |

## Browser live feeds

| Variable | Default | Description |
| :--- | :--- | :--- |
| `WS_PROTOBUF` | `false` | **Experimental.** Sends browser live feeds as binary Protobuf instead of JSON. Only the exact value `true` enables it; any other value means JSON. There is no automatic fallback between the formats. HTTP APIs and agent protocols are not affected. |

After changing `WS_PROTOBUF`, restart the server and reload open browser tabs. Deploy the server and its browser assets together. Developers: see [browser protocol](../develop/browser-protocol.md).

## Diagnostics

Both are off by default. With neither set, logs and open ports are the same as without them. See [troubleshooting](troubleshooting.md) for how to use them.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MEMORY_LOG` | `false` | Only the exact value `true` adds an hourly `[Maintenance] memory` log line with heap, GC and goroutine figures, realtime-cache and per-agent state counts, and TSDB block counts. |
| `PPROF_ADDR` | *none* (off) | Go profiling listener, e.g. `127.0.0.1:6060`. Must be a loopback host (`127.0.0.1`, `::1` or `localhost`); anything else stops startup. Served on its own port, separate from the app. Unset opens no port. |

## Fixed limits

These are not configurable:

| Limit | Value |
| :--- | :--- |
| HTTP header read timeout | 10 seconds |
| HTTP read and write timeouts | 30 seconds |
| HTTP idle timeout | 60 seconds |
| Form and JSON request bodies | 1 MiB |
| Agent HTTP submissions | 16 KiB |
| Collector WebSocket frames | 1 MiB |

[Security](security.md#limits) lists the remaining message size and connection limits.
