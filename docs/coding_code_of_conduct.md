# CertainStats — Coding Style Code of Conduct & Standards

> **Version:** 2.0 · **Date:** 2026-08-26 · **Scope:** Global Repository Architecture & Engineering Guidelines

---

## 1. Core Engineering Philosophy

CertainStats is a high-performance, dense, real-time infrastructure and fleet monitoring platform. Every line of code written in this repository must adhere to three foundational tenets:

1. **Performance & Low Overhead First**:
   - Zero or minimal allocations in hot paths (TSDB ingestion, metrics ingestion from agents, and WebSocket broadcast loops).
   - Fast sub-millisecond query responses and lightweight in-memory caches.
2. **Simplicity & Dependency Minimalism**:
   - Backend: Pure Go standard library architecture wherever feasible, avoiding bloated middleware frameworks.
   - Frontend: Modern, modular Vanilla JavaScript and CSS design tokens. Zero client-side runtime frameworks (React/Vue/Angular) in production dashboard paths.
3. **Deterministic State & Integrity**:
   - Lifetime counters (odometers) must never be conflated with or overwritten by short-interval rate deltas.
   - Concurrency must be strictly ordered to prevent deadlocks and data races.

---

## 2. Go Backend Standards

### 2.1 Project Structure & Package Isolation
- **`cmd/`**: Contains executable entrypoints only (e.g., `cmd/certainstats`). No business logic should reside directly in `cmd/` without abstraction.
- **`internal/`**: Private application code. Group packages logically by responsibility:
  - `internal/agent`: Agent submission handlers (`POST /submit`), provisioning, and Beszel WS integration.
  - `internal/agent_parser`: Dedicated payload parsers for `Beszel`, `LTstats`, and `HetrixTools` formats.
  - `internal/metrics`: Realtime cache and TSDB query handlers.
  - `internal/context`: Centralized cache registries (`DashboardCache`, `MetricsCache`, `StaticCache`) and lifecycle context.
  - `internal/store/sqlite`: Database access, idempotent migrations, and query operations.
  - `internal/auth`: Session token generation (`GenerateSessionToken`), password hashing, and session authentication.
  - `internal/routine`: Background timers (alert sweeps, synchronized pulses, hourly session cleanups).
  - `internal/minify`: Static asset minification, hashing, and SRI generation.
  - `internal/web`: Go template renderer, routing, and HTTP handlers.

### 2.2 Compilation & Build Rules
- **Never compile binaries directly into workspace roots.**
- When verifying Go builds in tooling, CLI scripts, or CI, **strictly target `/dev/null`**:
  ```bash
  go build -o /dev/null ./cmd/certainstats
  ```

### 2.3 Error Handling & Logging
- Handle errors explicitly and idiomatically at the call site:
  ```go
  if err != nil {
      return fmt.Errorf("agent %s: failed to increment traffic: %w", agentID, err)
  }
  ```
- Do not silently swallow errors. If an error is intentionally ignored, document the rationale with an explicit comment.
- Use structured logging via `internal/logger` for operational events, warnings, and errors.

### 2.4 Concurrency & Mutex Management
- Always use `sync.RWMutex` where reads vastly outnumber writes.
- Keep critical sections minimal. Do not perform expensive I/O or network requests while holding mutex locks.
- Establish a strict lock acquisition hierarchy across modules to eliminate deadlock risks.
- Propagate `context.Context` through all blocking operations and long-running routines to allow graceful cancellation.

### 2.5 Database Access (SQLite & TSDB)
- **Parameterized Queries**: Always use `?` placeholders for query arguments. String concatenation or interpolation in SQL statements is strictly forbidden.
- **Transactions**: Every transaction must defer a rollback and execute an explicit commit:
  ```go
  tx, err := s.db.BeginTx(ctx, nil)
  if err != nil {
      return err
  }
  defer tx.Rollback()
  
  // Executions...
  
  return tx.Commit()
  ```
- **Schema Changes**: All schema additions must be declared idempotently in `internal/store/sqlite/migrate.go`.

---

## 3. Frontend & UI Engineering Standards

### 3.1 Strict Inline JavaScript Restrictions (Zero Scripts in Partials)
To ensure clean separation of concerns, CSP (Content Security Policy) readiness, and subresource integrity (SRI):

1. **Zero `<script>` Tags in Partial Templates**:
   - **Never place `<script>` or `<style>` tags inside any partial template** (e.g. `web/templates/partials/agents/`, `web/templates/partials/dashboards/`, `web/templates/partials/common/`).
   - Partials must contain **pure HTML markup only**.
2. **Page Templates: Single Bootstrap Call Only**:
   - Top-level page templates (e.g. `agents_list.html`, `public_dashboard.html`) may only contain a single top-level `<script>` block at the end of the content body invoking a single `.init({ ... })` module function.
   - This bootstrap call must only pass server-rendered JSON configuration and metadata into the module:
     ```html
     <script src="{{.StaticPath}}/{{asset "js/admin_agents.js"}}" integrity="{{integrity "js/admin_agents.js"}}"></script>
     <script>
       window.CertainStatsAdminAgents.init({
         panelPath: '{{.PanelPath}}',
         agents: [ /* JSON serialization */ ]
       });
     </script>
     ```
3. **No Inline State or Global Variables in HTML**:
   - Variables like `let selectedProvisionType = 'docker';` or modal state must **never live in HTML templates**. All state belongs in the appropriate static JS module.
4. **Clean Event Delegation**:
   - Prefer attaching DOM event listeners (`addEventListener`) inside static JavaScript files. When using inline `onclick` attributes for backwards compatibility, call clean module namespace functions (e.g. `onclick="CertainStatsProvision.selectDriver('docker')"`). Never write multiline inline JavaScript blocks in attributes.

---

### 3.2 Modular Vanilla JavaScript Architecture
- Frontend scripts live in `web/static/js/`:
  - `telemetry.js` (`window.CertainStatsTelemetry`): WebSocket manager, SPA router, date/time pickers, formatters, toasts.
  - `chart.js` (`window.CertainStatsChart`): High-DPI Canvas time-series charting engine.
  - `admin_agents.js` (`window.CertainStatsAdminAgents`): Private Multi-Agent Hub and In-Page Detail SPA.
  - `public_dashboard.js` (`window.CertainStatsPublicDashboard`): Public status page SPA.
  - `provision_renderer.js` (`window.CertainStatsProvision`): Multi-driver provisioning, reinstall, and uninstall instructions.
- All modules must wrap their code in an IIFE (`(function () { 'use strict'; ... })();`) and export explicitly to `window`.

---

### 3.3 Unified In-Page SPA Routing (No Redundant Standalone Pages)
- **Single-View In-Page Architecture**:
  - The dashboard uses HTML5 History API (`pushState` and `popstate`) via `initRouter` to switch between Overview and In-Page Detail views seamlessly without full page reloads.
  - Routes: `/` displays Overview; `/{agent_id}` displays that node's `#agents-detail-view` in-place.
  - **No redundant standalone pages**: Do not maintain duplicate standalone detail views (e.g. `/agent/:id` is deprecated in favor of unified `/{agent_id}` in-page SPA routing).

---

### 3.4 Direct In-Place Editing (No Modal Popups for Simple Text)
- For metadata editing (Agent Notes, Nicknames):
  - Use **in-place click-to-edit interactions** (clicking directly on the text transforms it into an inline input/textarea with Save/Cancel controls).
  - Do not spawn popup modal dialogs for basic text or note modifications.
  - Note layout standard:
    - **Header Inline Widget**: Shows `| [No notes / short text / See below] notes` in the monitor header. Clicking opens an inline `<input>` right in the header bar.
    - **Expanded Notes Section**: Appears below specifications when a note is long (> 40 chars or multiline `\n`) with an in-place `<textarea>` editor.

---

### 3.5 Operational UI Standards (Zero Emojis & Modal Dismissal)
1. **Zero Emojis in Operational Controls**:
   - Operational dashboards, provisioning modals, terminal instruction copy blocks, and alert forms must **refrain from using emojis** (e.g. `📋`, `✓`, `📄`, `⚠️`, `⏳`).
   - Use clean, semantic inline SVGs and crisp typography.
2. **Click-to-Dismiss Modal Backdrops**:
   - Every modal popup overlay must support dismiss-on-overlay-click (clicking on the blurred background outside `.modal-content` immediately closes the modal).

---

### 3.6 In-Place Realtime DOM Mutation
- High-frequency WebSocket updates (e.g., 15-second telemetry snapshots) must **never wipe or rebuild entire DOM container trees** via `innerHTML = ...`.
- Mutate specific target elements in-place using `textContent`, `className`, and `style.width`:
  ```javascript
  const elUsed = document.getElementById('disk-used-' + safePath);
  if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(d.used_bytes);
  ```
- Instant transition: Render remembered state immediately from memory on view switches without showing blank loading flashes.

---

### 3.7 State Management & Odometer Separation
- **Lifetime Odometers**: Persistent cumulative statistics (Total Network Traffic, Total Disk Read/Write) are loaded from the database and maintained in state (`agentsData`).
- **Telemetry Deltas**: Incoming WebSocket ticks (`snap.disks`, `snap.rx_bps`, etc.) update live capacity percentages, throughput gauges, and live chart points. **They must never overwrite lifetime cumulative odometer counters with interval deltas.**
- Separate LocalStorage keys cleanly between private and public dashboards (e.g., `certainstats_active_hours` vs `certainstats_public_active_hours`).

---

### 3.8 Styling & Design Tokens
- All visual styles must utilize CSS custom properties defined in `styles.css`:
  - Backgrounds: `var(--bg-primary)`, `var(--bg-secondary)`, `var(--bg-panel)`
  - Borders: `var(--border-color)`, `var(--border-hover)`
  - Text: `var(--text-primary)`, `var(--text-secondary)`, `var(--text-muted)`
  - Accents: `var(--accent-primary)`, `var(--accent-secondary)`, `var(--accent-glow)`
- **Dark-First Design**: Dark mode is the primary canvas; light theme is a first-class tuned inversion.
- **Typography Standard**:
  - Monospace font (`JetBrains Mono`, `monospace`) must be used for all numeric metrics, rates, capacities, timestamps, terminal code, and hashes.
  - Sans-serif font (`Outfit`, `Inter`) for headings, labels, and UI controls.

---

## 4. Caching & Networking Standards

Refer to [`docs/caching_code_of_conduct.md`](caching_code_of_conduct.md) for full caching rules.

1. **Centralized Registries**: All cache lookups, storage, and invalidations must go through `internal/context`.
2. **Pre-Compression**: Cache entries store pre-compressed Zstandard (`zstd`) and Gzip (`gzip`) payloads to eliminate runtime compression overhead.
3. **No ETags on Telemetry**: Real-time telemetry endpoints must not compute SHA256 ETags, avoiding CPU waste and cache thrashing.
4. **Deterministic Invalidation**: Cache entries must be explicitly invalidated upon mutation (e.g., dashboard edits, agent revocations).

---

## 5. Anti-Patterns (Strictly Forbidden)

| Anti-Pattern | Reason & Consequence | Approved Pattern |
|---|---|---|
| ❌ `<script>` inside partial templates | Causes duplicate execution, leaks unmanaged state, breaks CSP/SRI. | Move all script logic to `web/static/js/*.js`. |
| ❌ Emojis in technical/provisioning UI | Clutters technical telemetry and breaks clean ops aesthetic. | Use clean inline SVGs and crisp typography. |
| ❌ Modal popup for simple note editing | Adds unnecessary friction and extra clicks. | Use direct in-place click-to-edit inline inputs and cards. |
| ❌ Redundant standalone page routes (`/agent/:id`) | Creates duplicate template maintenance and breaks SPA flow. | Use unified in-page SPA routing (`/:agent_id`). |
| ❌ `agent.disks = snap.disks;` | Overwrites lifetime cumulative GB with 15s tick delta (KB). | Update `d.used_bytes` in-place; preserve `d.read_bytes`. |
| ❌ `go build -o certainstats ./cmd/certainstats` | Pollutes repository root with compiled binary artifacts. | Use `go build -o /dev/null ./cmd/certainstats`. |
| ❌ Manual `innerHTML` wiping in live loops | Destroys active DOM nodes, causes layout thrashing and resets scroll. | Target specific element IDs with `textContent` or `style`. |
| ❌ Hardcoded colors (e.g., `#121318`) in HTML | Breaks light/dark theme switching and consistency. | Use CSS custom properties (`var(--bg-secondary)`). |
| ❌ Heavy frontend frameworks / CDN scripts | Introduces supply chain risks, bloat, and latency. | Use pure Vanilla JS + Go HTML templates. |
| ❌ Unparameterized SQL concatenation | Security risk (SQL injection) and poor query plan caching. | Always use parameterized queries (`?`). |

---

## 6. Testing & Verification Requirements

1. **Unit Tests**:
   - Every store and helper package must maintain working unit tests.
   - Run the complete test suite before submitting changes:
     ```bash
     go test ./...
     ```
2. **Compilation Integrity**:
   - All code must compile cleanly with zero errors or warnings:
     ```bash
     go build -o /dev/null ./cmd/certainstats
     ```
