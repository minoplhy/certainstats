# Environment Variables

Choose one way to place the panel and public dashboards; mixing them stops startup.

- **URL mode:** set `PANEL_URL`, and optionally `PUBLIC_URL`. Each side is served only on the host named in its URL, for browsers and agents alike, and any other host gets 404. Without `PUBLIC_URL`, dashboards are served at `/dashboard` on the panel's host.
- **Path mode:** set no URL, and optionally `PANEL_PATH`/`PUBLIC_PATH`. Both sides answer on any host.

`PUBLIC_URL` without `PANEL_URL` is an error, and so is a `*_PATH` other than its default alongside a URL. A `*_PATH` equal to its default (`/` or `/dashboard`) is ignored with a warning, so older compose files keep working.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | TCP port for the server to listen on. |
| `HOST` | `0.0.0.0` | Host/IP address to bind the server to. |
| `PANEL_URL` | *none* | URL mode: Admin Panel URL including its host, e.g. `https://admin.example.com` or `https://example.com/admin`. |
| `PUBLIC_URL` | `<PANEL_URL host>/dashboard` | URL mode: Public Dashboards URL including its host. Requires `PANEL_URL`. |
| `PANEL_PATH` | `/` | Path mode only: subpath to mount the Admin Panel. |
| `PUBLIC_PATH` | `/dashboard` | Path mode only: subpath to mount the Public Dashboards. |
| `DATA_DIR` | `./data` | Directory for database files (SQLite DB and TSDB). |
| `ALLOWED_ORIGINS` | *none* | Comma-separated list of allowed origins. |
| `WS_PROTOBUF` | `false` | Experimental browser Protobuf WebSockets. Only the exact value `true` enables it; all other values use JSON. Read once at startup; restart the server and reload open tabs after changing it. Compose forwards `.env` values; the application reads process environment only. |
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
| `LOGIN_ATTEMPTS_PER_MINUTE` | `10` | Per-IP refill rate for sign-in and first-time setup attempts. |
| `LOGIN_ATTEMPT_BURST` | `10` | Per-IP sign-in and setup attempt burst; further attempts get 429. |
| `RETENTION_DURATION` | `0` (unlimited) | TSDB retention in Go duration syntax, e.g. `2160h`. |
| `STORAGE_MAX_BYTES` | `0` (unlimited) | TSDB block retention size in bytes; not a whole-volume quota. |

`ALLOWED_ORIGINS` adds explicit HTTP(S) origins to same-origin browser WebSockets. Empty configuration no longer permits arbitrary origins. Forwarded HTTPS indicators require a trusted proxy. Browser HTTP defaults are a ten-second header timeout, thirty-second read/write timeouts and sixty-second idle timeout. Forms/JSON are limited to 1 MiB; agent submissions remain 16 KiB.

The container uses Alpine's default root user; no fixed UID/GID or volume-ownership migration is required. Initial setup uses the link printed in the server log at first start. See [security and rollout](security-and-rollout.md) for CSRF, recovery, caching and staged deployment details.
