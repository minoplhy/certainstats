# Environment Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | TCP port for the server to listen on. |
| `HOST` | `0.0.0.0` | Host/IP address to bind the server to. |
| `PANEL_URL` | *none* | Fully-qualified URL for the Admin Panel. (Default to `http://HOST:PORT`)|
| `PUBLIC_URL` | *none* | Fully-qualified URL for the Public Dashboards. (Default to `http://HOST:PORT/dashboard/`)|
| `PANEL_PATH` | `/` | **DEPRECATED** Subpath location to mount the Admin Panel. Use `PANEL_URL` instead. |
| `PUBLIC_PATH` | `/dashboard` | **DEPRECATED** Subpath location to mount the Public Dashboards. Use `PUBLIC_URL` instead. |
| `DATA_DIR` | `./data` | Directory for database files (SQLite DB and TSDB). |
| `ALLOWED_ORIGINS` | *none* | Comma-separated list of allowed origins. |
| `DEBUG` | `false` | Enable verbose trace logging. |
| `UPDATE_EVERY` | `60` | Metric sweep frequency (seconds). |
| `BESZEL_EVERY` | `60` | Beszel-agent metrics sweep frequency (syncs to `UPDATE_EVERY` by default). |

| Variable | Default | Description |
| :--- | :--- | :--- |
| `TRUSTED_PROXY_CIDRS` | empty | Comma-separated trusted proxy CIDRs. Forwarded headers from other peers are ignored. |
| `PUBLIC_REQUESTS_PER_MINUTE` | `1200` | Per-IP anonymous request refill rate. |
| `PUBLIC_REQUEST_BURST` | `120` | Per-IP anonymous request burst. |
| `PUBLIC_BUILDS_PER_MINUTE` | `60` | Per-IP uncached result-build refill rate. |
| `PUBLIC_BUILD_BURST` | `20` | Per-IP uncached build burst; identical followers share a build. |
| `RETENTION_DURATION` | `0` (unlimited) | TSDB retention in Go duration syntax, e.g. `2160h`. |
| `STORAGE_MAX_BYTES` | `0` (unlimited) | TSDB block retention size in bytes; not a whole-volume quota. |

`ALLOWED_ORIGINS` adds explicit HTTP(S) origins to same-origin browser WebSockets. Empty configuration no longer permits arbitrary origins. Forwarded HTTPS indicators require a trusted proxy. Browser HTTP defaults are a ten-second header timeout, thirty-second read/write timeouts and sixty-second idle timeout. Forms/JSON are limited to 1 MiB; agent submissions remain 16 KiB.

The container runs as UID/GID 10001; grant that account access to existing persistent bind mounts before upgrading. Initial setup credentials are read from `DATA_DIR/setup-token`, rather than application logs. See [security and rollout](security-and-rollout.md) for CSRF, recovery, caching and staged deployment details.
