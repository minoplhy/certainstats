# Security

This page explains how CertainStats protects the admin panel, public dashboards and agent connections, and which settings you control. It is for people running a server. The settings themselves are listed in [configuration](configuration.md). Implementation details are in [architecture](../develop/architecture.md).

## Hosts and reverse proxies

- **URL mode** (`PANEL_URL` set): each side is served only on the host named in its URL; any other host gets 404. **Path mode**: both sides answer on any host. Mixing the two is rejected at startup. See [configuration](configuration.md#where-the-panel-and-dashboards-live).
- Hostnames match case-insensitively.
- Forwarded headers are accepted only from proxies in `TRUSTED_PROXY_CIDRS`. From any other peer they are stripped before cookies or origin checks use them.
  - `X-Forwarded-Host` selects the host only when it comes from a trusted proxy.
  - Forwarded HTTPS indicators also require a trusted proxy.
  - The client address chain is read from right to left, through trusted hops only.

## Signing in and first-time setup

- **First start.** The server prints a setup link (`/first-time-setup?token=…`) in its log, once. The link pre-fills the setup form. The token is held only in memory and stops working once the first account exists. Setup finishes without a server restart.
- **Sign-in attempts** are rate-limited per IP (`LOGIN_ATTEMPTS_PER_MINUTE`, `LOGIN_ATTEMPT_BURST`; default 10 per minute, burst 10). An unknown username costs the same bcrypt work as a wrong password, so timing does not reveal which usernames exist.
- **Sessions end** in these cases, and their open browser connections close:
  - Logging out or being ejected revokes that session's live connections.
  - Changing a password updates the hash and deletes the user's other sessions in one step.

## Your account

On the **Settings** page:

- **Change password:** enter the current password and the new one twice. A password cannot be empty or longer than 72 bytes. Changing it signs out all your other devices.
- **Signed-in devices:** a table of your sessions with device, IP address and last activity. **Sign out** ends one session; **Sign out other devices** ends all but the current one.

Sessions last 24 hours, or 30 days with **Keep me signed in for 30 days** on the sign-in page. Expired sessions are removed every hour.

## Scripts and API clients (CSRF)

Every browser change, including sign-in, sign-out and first-time setup, needs a signed CSRF token. A script that drives the panel like a browser must:

1. Fetch a panel page with a cookie jar, and keep the `csrf_token` cookie.
2. Read the page's `csrf-token` meta value.
3. Send that value as the `X-CSRF-Token` header for JSON requests, or as a `csrf_token` field in URL-encoded forms.

Agent submissions authenticate with the agent's token and need no CSRF token.

Tokens are signed with a key that lives only in the server process. After a server restart, reload any open form before submitting it.

## Public dashboards

- A dashboard with missing or empty public access rules shows nothing publicly: its HTML, JSON, history and WebSockets are all denied.
- An empty checkbox selection stays empty; it never means "everything".
- Nested disk values (capacity, read and write) are controlled separately.
- In the admin panel, live snapshots contain only the signed-in user's own agents.
- How to create and configure status pages is in [status pages](dashboards.md).
- Changing a dashboard or its members closes affected public connections; open connections also re-check their access regularly.

## Browser hardening

- **Content Security Policy** allows scripts only from the server's own origin. Pages cannot be framed, and referrer and content-type protections are set.
- **No inline scripts.** All page code lives in separate asset files.
- **Assets are split by audience.**
  - The public side serves only `shared` and `public` assets, so status-page visitors cannot download admin scripts or styles.
  - The panel serves `admin` assets only to signed-in sessions (`Cache-Control: private`) and returns 404 otherwise.
  - Sign-in and setup pages load only `shared` and `auth` assets.
- **Cache headers.** Signed-in responses use `private, no-store`; dynamic public responses use `no-store`. These tell browsers not to cache; the server still caches internally.
- **Browser WebSockets** accept same-origin connections, plus any origins listed in `ALLOWED_ORIGINS`.

## Agents and collectors

- **Agent WebSockets** authenticate with the agent's credentials: its token, and for Beszel a per-agent SSH key. See [agents](agents.md).
- Resetting an agent's token or SSH key, or deleting the agent, closes its live connection.
- **Collectors** authenticate with a per-collector token before the WebSocket opens. The server stores only a hash of the token. A collector is disconnected when its token is reset or it is removed. See [collectors](collectors/README.md).

## Webhooks

Alert notifications are sent by webhook; see [alerts](alerts.md).


- Only HTTP(S) URLs are accepted, and URLs with embedded credentials are rejected.
- Requests time out after ten seconds.
- At most three redirects are followed, and never to a different host.
- Private (internal network) destinations are allowed, since only administrators configure them.

## Limits

### Rate limits

Per-IP quotas use a token bucket: a refill rate per minute plus a burst allowance.

| What | Default | Settings |
| :--- | :--- | :--- |
| Anonymous requests | 1,200/min, burst 120 | `PUBLIC_REQUESTS_PER_MINUTE`, `PUBLIC_REQUEST_BURST` |
| Uncached builds | 60/min, burst 20 | `PUBLIC_BUILDS_PER_MINUTE`, `PUBLIC_BUILD_BURST` |
| Sign-in and setup attempts | 10/min, burst 10 | `LOGIN_ATTEMPTS_PER_MINUTE`, `LOGIN_ATTEMPT_BURST` |

- Identical requests that arrive during a build share its result and use no further build quota.
- At most 10,000 IP addresses are tracked. When the table is full, IPs idle for ten minutes are dropped, and new IPs share an overflow budget.
- An exhausted quota returns **429**. Saturated server capacity (below) returns **503**. Both include `Retry-After`.
- These defaults are starting points to tune, not measured capacity.

### Query capacity

| What | Limit |
| :--- | :--- |
| TSDB queries running at once | 32 overall, 16 for public history |
| Requests waiting for a query or a shared build | 64, shared |
| Public request deadline while waiting | 2 seconds |

### Connections

| What | Limit |
| :--- | :--- |
| Browser connections | 1,024 total, 64 per user, 256 per dashboard |
| Live collector connections | 256 |
| Collector connection attempts | 30 per minute per IP |

All WebSocket writes have deadlines, so a stalled client cannot hold the server up.

### Message sizes

| What | Limit |
| :--- | :--- |
| Browser WebSocket messages | 4 KiB |
| Agent HTTP submissions | 16 KiB |
| Beszel agent WebSocket messages | 1 MiB (detailed stats exceed 16 KiB) |
| Collector WebSocket frames | 1 MiB |
| Form and JSON request bodies | 1 MiB |

HTTP timeouts are listed in [configuration](configuration.md#fixed-limits).
