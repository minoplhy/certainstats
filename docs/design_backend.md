# CertainStats — Design Document

> **Version:** 1.0
> **Date:** 2026-07-20
> **Author:** Minoplhy / AI-assisted
> **License:** MIT

---

## 1. Project Overview

**CertainStats** is a self-hosted, single-binary server monitoring platform. It collects real-time system metrics (CPU, RAM, disk, network) from remote agents, stores them in a Prometheus-compatible TSDB, and exposes them through two distinct frontends:

| Frontend | Purpose | Path |
|---|---|---|
| **Admin Panel** (`frontend-admin`) | Private operator UI — agents, alerts, dashboards, user settings | `/` (configurable) |
| **Public Dashboard** (`frontend-public`) | Unauthenticated embeddable view per dashboard slug | `/dashboard` (configurable) |

The backend is a single Go binary (`certainstats`) using:
- **chi v5** HTTP router
- **Prometheus TSDB** for time-series storage
- **SQLite** (via `modernc.org/sqlite`) for agent state, sessions, alerts, dashboards
- **WebSockets** for real-time agent data push to the admin UI
- **CBOR** encoding for agent submissions

---

## 2. Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   certainstats binary                    │
│                                                          │
│  ┌─────────────────┐    ┌────────────────────────────┐  │
│  │  Admin Panel    │    │   Public Dashboard SPA      │  │
│  │  (SSR + In-Page)│    │   (SSR + In-Page, no-auth)  │  │
│  │  /              │    │   /dashboard/               │  │
│  └────────┬────────┘    └────────────┬───────────────┘  │
│           │ REST + WS                │ REST + WS         │
│  ┌────────▼────────────────────────▼───────────────┐    │
│  │              Go HTTP (chi v5)                    │    │
│  │   /submit   /api/*   /api/public/*   /api/ws    │    │
│  └──────────────┬──────────────────────────────────┘    │
│                 │                                        │
│  ┌──────────────▼──────────────────┐                    │
│  │  internal/                      │                    │
│  │   agent/        — provision, submit, heartbeat       │
│  │   agent_parser/ — Beszel, LTstats, HetrixTools       │
│  │   alert/        — rule engine, retry, notifications  │
│  │   auth/         — session, login/logout, setup       │
│  │   dashboard/    — access rules, public sharing       │
│  │   metrics/      — real-time cache, TSDB query        │
│  │   store/sqlite/ — SQLite repository impls            │
│  │   ws/           — UI broadcaster, public WS          │
│  │   routine/      — background sweep goroutine         │
│  │   web/          — SSR HTML templates, in-place router│
│  │   minify/       — asset minification & SRI hashes    │
│  └──────────────┬──────────────────┘                    │
│                 │                                        │
│  ┌──────────────▼──────────────────┐                    │
│  │  SQLite (agent_state.db)        │                    │
│  │  Prometheus TSDB (./data/tsdb)  │                    │
│  │  Context In-Memory Cache (RAM)  │                    │
│  └─────────────────────────────────┘                    │
└─────────────────────────────────────────────────────────┘
          ▲                    ▲                  ▲
          │ Custom Binary      │ Gzip/Base64 JSON │ WebSocket (CBOR)
  ┌───────┴──────┐     ┌───────┴────────┐ ┌───────┴────────┐
  │   LTstats    │     │  HetrixTools   │ │  Beszel Agent  │
  │    Agent     │     │     Agent      │ │     (WS)       │
  └──────────────┘     └────────────────┘ └────────────────┘
```

### 2.1 Data Flow

1. **Agent → Server:**
   - **LTstats Agents**: Push custom packed binary payloads (`POST /submit`).
   - **HetrixTools Agents**: Push gzipped, base64-encoded JSON payloads (`POST /submit`).
   - **Beszel Agents**: Connect via WebSocket (`GET /api/beszel/agent-connect`) using CBOR serialization.
2. **Server → TSDB:** The `agent_parser` registry automatically detects the agent format, decodes raw metrics into standard time-series data, and appends them to Prometheus TSDB.
3. **Server → SQLite:** Heartbeat timestamps, online status, hardware metadata, and cumulative traffic odometers are upserted into SQLite.
4. **Server → Admin UI (WS):** The `uiBroadcaster` pushes live `AgentSnapshot` deltas to authenticated admin WebSocket connections (`/api/ws`).
5. **Admin UI → Server:** REST calls for agent management, dashboard configuration, alert rules, and settings.
6. **Public UI → Server:** Calls to `GET /api/public/dashboard/{slug}` and `/api/public/metrics` which enforce per-dashboard `AccessRule` field filtering.

---

## 3. Dual-Frontend Deployment Modes

CertainStats supports three deployment topologies:

| Mode | Panel Host | Public Host | Description |
|---|---|---|---|
| **Combined** (default) | same | same | Both frontends on one host, path-split (`/` + `/dashboard`) |
| **Virtual Host** | different | different | Each frontend on its own domain (recommended for production) |
| **Custom Path** | — | — | Both on same host, configurable sub-paths |

Configuration via environment variables — see [`environment.md`](environment.md).

---

## 4. Agent Drivers & Protocols

CertainStats supports 3 dedicated agent drivers:

| Driver | Transport | Format | Auth | Notes |
|---|---|---|---|---|
| `beszel` | WebSocket `GET /api/beszel/agent-connect` | CBOR | Token (`X-Token` header) | Beszel agent protocol over WebSocket; pulls telemetry on-demand |
| `ltstats` | HTTPS `POST /submit` | Custom packed binary | Token embedded in payload | High-efficiency custom binary layout (`NetHeader`, `Details`, `StatT`) |
| `hetrixtools` | HTTPS `POST /submit` | Gzip + Base64 JSON | Token in payload | HetrixTools agent compatible format |

Each agent has a unique `agent_id` (ULID), a human-readable `nickname`, and carries the following hardware snapshot metadata:

- `cpu_model`, `cpu_cores`
- `ram_size`, `swap_size`
- `disk_size`, multi-disk mount points and partitions
- `linux_version`, `uptime`
- Network traffic odometers (`rx_bytes`, `tx_bytes`)
- Storage I/O odometers (`disk_read_bytes`, `disk_write_bytes`)

---

## 4.1 Authentication & Session Architecture

Authentication is unified across both the JSON API (`/api/auth/login`) and the server-rendered Web UI (`/login`):

- **Token Generation**: Cryptographically secure 32-byte URL-safe base64 session tokens generated via `auth.GenerateSessionToken()`.
- **Session Duration**:
  - **Default**: **24 hours** (`24 * time.Hour`) for standard logins.
  - **Remember Me**: **30 days** (`30 * 24 * time.Hour`) when `remember` is requested via form checkbox or API payload.
- **Cookie Security**:
  - Name: `session_token`
  - Flags: `HttpOnly`, `SameSite=Lax`, `Path=/`, dynamic `Secure` flag (enabled over direct HTTPS or when behind `X-Forwarded-Proto: https` proxies).
  - Expiration: Synchronized with database `expires_at`.
- **Session Lifecycle & Management**:
  - Validated on every authenticated request via `requireAuth` (API) and `RequireAuthWeb` (Web).
  - Active sessions track `user_id`, `ip_address`, `user_agent`, `created_at`, `last_connected_at`, and `expires_at`.
  - Sessions can be audited and individually ejected or bulk-revoked ("Eject Other Sessions") from `/settings`.
  - Expired sessions are automatically purged hourly by the background routine (`routine.go`).

---

## 5. Metrics

### 5.1 Available Metrics

| Metric Key | Description | Unit |
|---|---|---|
| `agent_cpu_usage` | CPU utilization | % |
| `agent_cpu_iowait` | CPU IO wait | % |
| `agent_cpu_steal` | CPU steal | % |
| `agent_ram_used` | RAM consumption | bytes |
| `agent_swap_used` | Swap consumption | bytes |
| `agent_disk_used` | Disk usage (root) | bytes |
| `agent_disk_read_bytes` | Disk read throughput | bytes/s |
| `agent_disk_write_bytes` | Disk write throughput | bytes/s |
| `agent_rx_bytes` | Network receive throughput | bytes/s |
| `agent_tx_bytes` | Network transmit throughput | bytes/s |

### 5.2 Storage

- **Real-time cache** (`internal/metrics`): In-memory ring buffer for live WebSocket pushes. No persistence.
- **TSDB** (Prometheus): Persistent time-series. Configurable retention (default: unlimited). All historical chart queries hit TSDB.

### 5.3 Query Endpoint

`GET /api/metrics?metric=<key>&agent_id=<id>&start=<unix_ms>&end=<unix_ms>`

Returns `MetricResponse { metric: string, series: MetricSeries[] }` where each series contains `[timestamp_ms, value]` tuples and a `labels` map for per-disk/per-NIC disambiguation.

---

## 6. Alert System

### 6.1 Trigger Types

| Trigger | Condition |
|---|---|
| `agent_down` | Agent goes offline (no heartbeat) |
| `cpu_usage` | CPU % > threshold for duration |
| `cpu_iowait` | CPU IO Wait % threshold |
| `cpu_steal` | CPU Steal % threshold |
| `ram_usage` | RAM % threshold |
| `swap_usage` | Swap % threshold |
| `disk_usage` | Disk % threshold |
| `net_rx` / `net_tx` | Network throughput KB/s threshold |
| `disk_read` / `disk_write` | Disk IO KB/s threshold |

### 6.2 Notification Actions

| Type | Description |
|---|---|
| `webhook` | Arbitrary HTTP POST with configurable JSON payload |
| `discord` | Discord webhook (pre-formatted embed) |
| `preset` | Reference a saved `AlertTarget` by ID (reusable) |

### 6.3 Alert Lifecycle

```
IDLE → FIRING (threshold breached for duration)
     → notification sent → status: sent / failed
FIRING → RESOLVED (metric returns to normal)
       → resolve notification sent
FAILED → RETRY available (manual or automatic)
```

Alert history is stored in SQLite with full event log (trigger time, resolve time, breach value, notification status, error message).

---

## 7. Public Dashboards

Public dashboards allow sharing a curated subset of agent data with unauthenticated viewers.

### 7.1 Access Rules (`accessrules`)

Each dashboard has an `AccessRule` that specifies which metric fields are visible. This is validated before any data is returned. Operators can restrict public views to only show, e.g., CPU + RAM, hiding disk/network data.

### 7.2 Agent Aliasing

Public agents are exposed with a `public_id` and a configurable `alias` (nickname override per dashboard), hiding the internal `agent_id`.

### 7.3 Public WebSocket

`GET /api/public/ws/{dashboard_id}` — live real-time push of agent snapshots to public dashboard viewers, filtered by `AccessRule`.

---

## 8. UI/UX Design

### 8.1 Design Philosophy

CertainStats targets a **premium, minimal, dark-first** aesthetic that feels at home alongside tools like Grafana Cloud, Datadog, and Fly.io dashboards. The design prioritizes:

- **Clarity at a glance** — status, health, and trends readable within 1 second
- **Zero-clutter density** — show maximum data with minimum noise
- **Real-time feel** — live WebSocket updates with micro-animation flash feedback (`pulse-flash` class)
- **Dark & Light themes** — `data-theme` attribute on `<html>`, persisted in `localStorage`

### 8.2 Color System

| Token | Dark value | Light value | Usage |
|---|---|---|---|
| `--bg-primary` | `#090a0f` | `#f8f9fa` | Page background |
| `--bg-secondary` | `#111827` | `#ffffff` | Cards, panels |
| `--border-color` | `rgba(255,255,255,0.07)` | `rgba(0,0,0,0.08)` | All borders |
| `--accent-primary` | `#3b82f6` | `#2563eb` | Buttons, active state, highlights |
| `--accent-glow` | `rgba(59,130,246,0.1)` | `rgba(37,99,235,0.08)` | Badge backgrounds |
| `--text-primary` | `#f1f5f9` | `#0f172a` | Primary text |
| `--text-secondary` | `#94a3b8` | `#475569` | Secondary text |
| `--text-muted` | `#475569` | `#94a3b8` | Captions, labels |
| `--status-online` | `#10b981` | `#059669` | Online indicator |
| `--status-offline` | `#ef4444` | `#dc2626` | Offline / danger |
| `--font-display` | `"Inter"` | — | Headings |
| `--font-mono` | `"JetBrains Mono"` | — | Metrics, IDs, code |

### 8.3 Metric Color Coding (consistent across all charts and bars)

| Metric | Color |
|---|---|
| CPU Used | `#3b82f6` (blue) |
| CPU IO Wait | `#fb923c` (orange) |
| CPU Steal | `#ef4444` (red) |
| RAM Used | `#14b8a6` (teal) |
| Swap Used | `#4b5563` (slate) |
| Disk Used | `#a855f7` (purple) |
| Network RX | `#1e40af` (dark blue) |
| Network TX | `#7e22ce` (dark purple) |
| Disk Read | `#f59e0b` (amber) |
| Disk Write | `#ef4444` (red) |

### 8.4 Typography

```css
--font-display: 'Inter', 'SF Pro Display', system-ui, sans-serif;
--font-mono:    'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace;
```

- Page titles: `28px`, `font-weight: 700`, `letter-spacing: -0.02em`
- Card headings: `18px`, `font-weight: 800`
- Labels: `11px`, `font-weight: 700`, `text-transform: uppercase`, `letter-spacing: 0.05em`
- Metric values: monospace, `font-weight: 600`

### 8.5 Motion & Animation

| Animation | Usage | Duration |
|---|---|---|
| `animate-fade-in` | Page-level component mounts | 0.3s ease |
| `pulse-flash` | Card flash on live metric update | 0.4s ease |
| `menuExpand` | Popover menus | 0.12s cubic-bezier(0.16, 1, 0.3, 1) |
| `fadeIn` (modals) | Modal appear | 0.3-0.4s cubic-bezier |
| `spin` | Loading spinners | 1s linear infinite |
| hover lift | Agent cards on hover | `translateY(-4px)` 0.3s |

---

## 9. UI Mockups

### 9.1 Agent Hub — Main View + Agent Detail

The Agent Hub is the primary landing page of the admin panel. It presents:

- **Left sidebar** with compact per-agent status and mini CPU/RAM bars for at-a-glance scanning
- **Main area** with "Agent Hub" title, node count badge, "Add Agent" CTA
- **Toolbar row** with search input and grid/list toggle
- **Agent cards** (grid mode, detailed or simplified density) displaying:
  - Agent name, online/offline status dot with ambient glow
  - CPU model and OS version (in detailed mode)
  - `UsageBar` components for CPU (stacked: used + iowait + steal), RAM (used + swap), Disk (per mount), Network (RX + TX), Disk IO (read + write)
  - Uptime and agent type in monospace footer
  - Hover: lift + accent border glow
- **Agent Detail panel** (right/secondary view) showing:
  - Recharts `AreaChart` and `LineChart` for each metric group
  - Downtime intervals shaded in red (`ReferenceArea`)
  - Drag-to-zoom via mouse/touch interaction
  - Time range selector (1h / 6h / 24h / 7d)

### 9.2 Alerts Panel + Public Dashboard

![Alerts Panel and Public Dashboard](mockup-alerts-public.png)

**Top — Alerts Panel:**

- Active Incidents banner (red-bordered card) for any currently-firing or failed alert agents
- Configured alert list with enable/disable toggles, trigger icon, condition string, edit/delete controls
- Recent Alert Events table with paginated history, search, status filter (All / Firing / Resolved)
- Retry button for failed notification deliveries

**Bottom — Public Dashboard (unauthenticated):**

- Accessible at `/{public_path}/{slug}` — no login required
- Shows alias-resolved agent cards with filtered metrics
- Live area chart for the full dashboard aggregate
- "Shareable Link" copy button, last-updated timestamp

---

## 10. Admin Panel Page Inventory

| Route | Component | Description |
|---|---|---|
| `/` | `AdminPanel` → `AgentView` | Agent Hub grid/list |
| `/agent/:id` | `AdminPanel` → `AgentDetailView` | Agent telemetry charts |
| `/dashboards` | `AdminPanel` → `DashboardsPanel` | Public dashboard list |
| `/dashboards/create` | `DashboardCreateView` | New dashboard wizard |
| `/dashboards/edit` | `DashboardEditView` | Edit dashboard agents/fields |
| `/alerts` | `AdminPanel` → `AlertsPanel` | Alert rules + history |
| `/alerts/create` | `AlertCreateView` | New alert rule form |
| `/alerts/edit` | `AlertEditView` | Edit existing alert |
| `/targets/create` | `TargetCreateView` | New notification preset |
| `/targets/edit` | `TargetEditView` | Edit notification preset |
| `/agents/management` | `ManagementView` | Token/SSH key management table |
| `/settings` | `SettingsView` | Password change, session management |
| `/login` | `LoginView` | Authentication |
| `/first-time-setup` | `FirstTimeSetupView` | Initial user creation |

---

## 11. Data Store Schema (Conceptual)

### SQLite Tables

| Table | Key Fields | Purpose |
|---|---|---|
| `agents` | `agent_id`, `user_id`, `token_hash`, `nickname`, `is_online`, `last_seen`, hardware columns | Core agent registry |
| `agent_traffic` | `agent_id`, `total_rx_bytes`, `total_tx_bytes`, disk odometers | Cumulative traffic counters |
| `beszel_ssh` | `agent_id`, `public_key`, `private_key` | Beszel SSH identity per agent |
| `sessions` | `token_hash`, `user_id`, `last_connected`, `expires_at` | Web session management |
| `users` | `user_id`, `username`, `password_hash`, `is_admin` | Admin accounts |
| `alerts` | `alert_id`, `user_id`, `nickname`, `enabled`, `trigger_json`, `action_json` | Alert rule definitions |
| `alert_agents` | `alert_id`, `agent_id`, `status`, `error_message` | Per-agent alert state |
| `alert_history` | `history_id`, `alert_id`, `agent_id`, `triggered_at`, `resolved_at`, `trigger_value`, `notified_status` | Event log |
| `alert_targets` | `target_id`, `user_id`, `name`, `type`, `destination`, `payload` | Reusable notification destinations |
| `dashboards` | `dashboard_id`, `user_id`, `slug`, `title`, `max_days`, `allowed_fields_json` | Public dashboard configs |
| `dashboard_agents` | `dashboard_id`, `agent_id`, `alias`, `sort_key` | Dashboard ↔ Agent membership |

---

## 12. Backend Packages

| Package | Responsibility |
|---|---|
| `cmd/certainstats` | Entry point, router wiring, virtual-host dispatch, config, embedded asset mounting, heartbeat sweeper |
| `internal/agent` | Provision, submit handler (`POST /submit`), list, rename, revoke, token reset, Beszel WS handler |
| `internal/agent_data` | Shared time diff constants, token generation, string helpers |
| `internal/agent_parser` | Parser registry; dedicated decoders for Beszel (CBOR), LTstats (custom binary), HetrixTools (gzip JSON) |
| `internal/alert` | Alert CRUD handlers, rule evaluation, notification dispatch, retry logic |
| `internal/auth` | Session management, password hashing, login/logout handlers, `requireAuth` middleware, first-time setup |
| `internal/compress` | Brotli/gzip response compression middleware |
| `internal/context` | Global in-memory cache registries (`DashboardCache`, `DashboardHTMLCache`, `MetricsCache`, `StaticCache`) |
| `internal/dashboard` | Dashboard CRUD handlers, access rule enforcement, public endpoint |
| `internal/lifecycle` | Graceful shutdown channel & server restart trigger |
| `internal/logger` | Structured leveled logger |
| `internal/metrics` | `RealtimeCache` (in-memory), TSDB query handler, public metrics handler |
| `internal/minify` | Static asset minification (HTML/CSS/JS), content fingerprinting, and W3C SRI hash calculation |
| `internal/notifications` | Webhook + Discord embed delivery implementations with retry worker |
| `internal/response` | Shared JSON API response helpers |
| `internal/routine` | Background goroutine: alert sweep, hourly session cleanup, offline marking, synchronized UI pulse |
| `internal/store` | Store interfaces (`AgentStore`, `AlertsStore`, `DashboardStore`, `UserStore`, `SessionStore`, `FullStore`) |
| `internal/store/sqlite` | SQLite implementations of all store interfaces + idempotent schema migrations |
| `internal/web` | Go template renderer, server-rendered views, in-page SPA routing, session/settings handlers |
| `internal/ws` | `Manager` (Beszel WS), `AgentBroadcaster` (UI push), UI handler, public WS handler |

---

## 13. Future Improvements & Roadmap

### High Priority

- [ ] **Multi-user support** — Currently single-user per instance. Add per-user agent ownership, invite system.
- [ ] **Alert grouping / silence rules** — Group related alerts, add maintenance window silencing.
- [ ] **Notification channels expansion** — Slack, PagerDuty, email (SMTP), Telegram.
- [ ] **Retention policy UI** — Let operators configure TSDB retention duration from the admin panel.

### Medium Priority

- [ ] **Agent auto-discovery** — Passive subnet scanner or push-based registration without pre-provisioning.
- [ ] **Dashboard access tokens** — Time-limited or password-protected public dashboard access.
- [ ] **Mobile-optimized public dashboard** — Touch-first PWA for the public view.
- [ ] **Aggregate metrics panel** — Cross-agent total compute/network summary view.
- [ ] **Annotations** — Mark deployments or incidents on the timeline charts.
- [ ] **Export** — CSV/JSON export for TSDB query results.

### Low Priority / Experiments

- [ ] **Prometheus scrape endpoint** — Allow external Prometheus to scrape agent metrics from CertainStats.
- [ ] **OpenTelemetry receiver** — Accept OTLP spans/metrics in addition to the custom agent protocol.
- [ ] **Plugin/extension system** — Allow third-party agent parsers via a plugin API.

---

## 14. Development Setup

### Prerequisites

- Go 1.26+
- Node.js 22+ / npm
- Docker (optional, for containerized build)

### Running in Development

```bash
# Backend (with hot reload via air or go run)
cd workspace/certainstats
go run ./cmd/certainstats

# Admin frontend
cd frontend-admin
npm install
npm run dev

# Public frontend
cd frontend-public
npm install
npm run dev
```

Set `PANEL_PATH=/` and `PUBLIC_PATH=/dashboard` (defaults). The Go binary serves the built frontends from embedded FS in production (`-tags embed`).

### Running Tests

```bash
go test ./...
```

### Docker Build

```bash
docker build -t certainstats:latest .
docker run -p 8080:8080 -v ./data:/app/data certainstats:latest
```

---

## 15. Configuration Reference

See [`environment.md`](environment.md) for the full environment variable reference.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Listening port |
| `HOST` | `0.0.0.0` | Bind address |
| `DATA_DIR` | `./data` | SQLite + TSDB storage path |
| `UPDATE_EVERY` | `60` | Metric sweep interval (seconds) |
| `BESZEL_EVERY` | `60` | Beszel sweep interval |
| `PANEL_URL` | — | Full URL of admin panel (for display in logs) |
| `PUBLIC_URL` | — | Full URL of public dashboard |
| `ALLOWED_ORIGINS` | — | CORS allowed origins (comma-separated) |
| `DEBUG` | `false` | Verbose trace logging |
