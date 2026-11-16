# Frontend

This page describes the browser side of CertainStats: design rules, design tokens, components, JavaScript modules, templates and routes. It is for developers changing `web/`. The current design is the light-first "Editorial" look, with a tuned dark theme, a deep teal accent, plain-language page headlines and self-hosted fonts.

## Design principles

> **"Calm, legible, real-time. Dependency-free."**

CertainStats renders pages on the server with Go `html/template` and makes them live with small vanilla JS modules. There is no npm package or frontend framework at runtime. The larger scripts are written as ES modules under `web/src/js` and bundled with esbuild into checked-in files, so Node is needed only to regenerate them. See [frontend JavaScript build](frontend-js-build.md).

Browser live feeds default to JSON. Experimental `WS_PROTOBUF=true` loads a locally bundled Protobuf decoder, whose generated files are checked in. See [browser protocol](browser-protocol.md).

### Core rules

1. **Say the state in words first.** Each main page opens with a headline that states the situation ("1 node is offline. The other 5 are healthy."), then shows the numbers.
2. **Hybrid server render + in-page navigation.** HTML is rendered on the server in under 1ms. Opening an agent detail swaps views in place with `pushState`, without a full page load.
3. **Light-first, dark as an equal.** Light is the default palette. Dark is tuned separately rather than inverted. The user picks **system**, **light** or **dark**, and system is the default.
4. **Color means something.** Teal is the only accent and is used for actions and focus. Green, amber and red appear only for status. Series colors only appear in charts and meters.
5. **Monospace for telemetry.** Every metric value, timestamp, ID and command uses Geist Mono. Headings use Bricolage Grotesque and UI text uses Figtree.
6. **No emojis or glyph icons.** Icons are inline SVG with `aria-hidden="true"`. Decorative Unicode glyphs (⇅ ⛁ ☁) are not used.
7. **Accessible by default.** Visible `:focus-visible` rings, native `<dialog>` modals, labelled controls, `aria-live` status regions and `prefers-reduced-motion` support.
8. **Custom canvas charts.** `CertainStatsChart` draws on HTML5 Canvas and reads every color from CSS tokens, so charts follow the theme.

## Design tokens

All tokens are defined at the top of [`web/static/shared/css/base.css`](../../web/static/shared/css/base.css), which is the source of truth for their values. Light values sit on `:root`, and dark values on `[data-theme="dark"]`. Components reference tokens only; templates and scripts contain no colour literals.

### Colours

The light theme uses a near-white page with white surfaces; the dark theme uses a deep blue-green page. The accent is teal (`#0e7c6b` light, `#3cc4ac` dark).

| Token | Role |
|---|---|
| `--bg` | Page background |
| `--surface` | Cards, tables, dialogs |
| `--surface-2` | Hover rows, segmented control track, code chips |
| `--line` / `--line-strong` | Borders and dividers |
| `--text` / `--text-2` / `--muted` | Primary, secondary, tertiary text |
| `--accent` / `--accent-soft` | Primary buttons, links, focus, selection |
| `--ok` / `--ok-soft` | Online, delivered, operational |
| `--warn` / `--warn-soft` | Partial outage, disk ≥ 90% |
| `--bad` / `--bad-soft` | Offline, firing, destructive actions |
| `--grid` / `--track` | Chart gridlines, meter tracks |

Legacy names (`--bg-primary`, `--accent-primary`, `--metric-cpu`, `--status-online`, …) are kept as aliases of these tokens for compatibility. New code should use the names above.

### Series colours

Fixed per metric across meters, legends, tooltips and charts:

| Token | Used for |
|---|---|
| `--s1` | CPU user, disk used, sparklines (teal) |
| `--s2` | CPU I/O wait, disk read (amber) |
| `--s3` | CPU steal, disk write (red) |
| `--s4` | RAM used (blue) |
| `--s5` | Swap used (grey) |
| `--rx` / `--tx` | Network download / upload (teal / blue) |

Swatch helpers `.sw-s1` … `.sw-tx` color legend marks. Scripts pass series colors as token names (`'--s1'` or `'var(--s1)'`), which `CertainStatsChart.resolveColor` turns into the current theme's value.

### Typography

Fonts are self-hosted as Latin-subset variable woff2 files in [`web/static/shared/fonts/`](../../web/static/shared/fonts) (SIL OFL, license files alongside). Public pages make no third-party font requests.

```css
--font-head: 'Bricolage Grotesque', 'Figtree', system-ui, sans-serif;
--font-body: 'Figtree', system-ui, -apple-system, 'Segoe UI', sans-serif;
--font-mono: 'Geist Mono', ui-monospace, 'SFMono-Regular', Menlo, monospace;
```

| Role | Size | Weight | Family | Usage |
|---|---|---|---|---|
| Hero headline (`.hero-title`, `.detail-title`, `.status-title`) | `clamp(26px, 3.4vw, 38px)` | 650 | Head | Page-opening sentence or entity name |
| Page title (`.page-title`) | 30px (24px mobile) | 650 | Head | Form pages |
| Section title (`.section-title`, `.chart-header-title`) | 16–18px | 650 | Head | Sections, chart cards, dialogs |
| Body | 14px | 400–600 | Body | Text, tables, forms |
| Label / hint | 12–13px | 500 | Body | Field labels, hints, eyebrow lines |
| Telemetry figure | 17–28px | 500 | **Mono** | Hero stats, agent metrics, tiles |
| Data / code | 11–13px | 400–500 | **Mono** | IDs, rates, axis labels, commands |

### Shape and spacing

- Radius: `--r` 10px (controls), `--r-lg` 16px (cards, dialogs), `--r-sm` 6px (small buttons, chips).
- Content column: `--content-w` 1180px, with a 32px side gutter on desktop and 16px on mobile.
- Shadows are only used for elevated surfaces (dialogs, popovers, tooltips): `--shadow-pop`.

## Components

### Page openers
- **`.hero`**: an eyebrow line (`.hero-eyebrow`), a sentence headline (`.hero-title`), optional `.page-subtitle`, `.hero-stats` and `.hero-actions`. Used on Agent Hub, Status pages, Alerts, Management and Settings.
- **Agent Hub headline**: rendered on the server by the `hub_headline` template (`partials/agents/headline.html`) from `OnlineCount` and `OfflineCount`, and re-rendered by `renderHubHeadline()` in `admin_agents.js` after a metadata sync. Offline wording uses `.is-bad`.
- **`.hero-stats`**: fleet figures (bandwidth now, disk I/O now, traffic and disk all time). They keep the `admin-live-*` and `admin-total-*` element IDs that `renderClusterStats` writes to, with `pub-*` IDs on the public page.

### Cards
- **`.card`**: surface, 1px `--line` border, `--r-lg` radius, 20px padding. Cards carry no shadow.
- **`.agent-card-item`**: an Agent Hub card containing:
  - a name with status dot, CPU model and driver;
  - a status badge;
  - three large figures (CPU %, memory %, disk %), each with a thin meter;
  - a 24h CPU sparkline (`canvas.agent-spark`);
  - a footer with live network rates and uptime.

  Offline cards add `.is-offline`, which applies a `--bad-soft` wash.
- **`.chart-card`**: a chart container with a title, legend (`.chart-legend-pills`) and `.chart-container` canvas. `.chart-card-wide` spans both columns of `.chart-grid`.
- **`.partition-card`**: per-mount storage card, rendered by `CertainStatsTelemetry.partitionCardHtml()`, which both detail views share.
- **`.hw-card`**: a detail metric tile with a label, capacity, live value and meter.

### Buttons

| Class | Appearance | Purpose |
|---|---|---|
| `.btn-primary` | Solid `--accent`, `--accent-fg` text | One primary action per area |
| `.btn-secondary` | Surface with `--line` border | Secondary actions |
| `.btn-ghost` | Transparent, muted text | Tertiary actions, icon buttons |
| `.btn-danger` | Red text and border, fills red on hover | Destructive actions |
| `.btn-sm`, `.btn-icon`, `.btn-block` | Size modifiers | Row actions, icon-only buttons, full width |

Button labels say exactly what happens ("Create status page", "Sign out other devices").

### Status
- **`.status-dot`**: 8px dot with a soft ring. Classes: `.online`, `.offline`.
- **`.badge`**: pill chip. Variants: `.badge-online`, `.badge-offline` (each with a leading dot), `.badge-warn` and `.badge-accent` (`.badge-indigo` is an alias).
- **`.banner`**: full-width status strip. Variants: `.is-ok`, `.is-warn`, `.is-bad`. It's used for the public "All systems operational / Partial outage" banner and the Alerts incident banner.

### Meters
`.usage-bar-track` with `.usage-segment` children. Segments start at zero width, and JS sets `style.width`.

- Series classes: `.seg-cpu-usr`, `.seg-cpu-io`, `.seg-cpu-stl`, `.seg-ram-used`, `.seg-ram-swap`, `.seg-disk` (`.is-high` above 90%), `.seg-net-rx` and `.seg-net-tx`.
- A track with `data-tooltip-rows` shows the floating tooltip, whose row colors are CSS values such as `var(--s2)`.

### Controls
- **Segmented control**: `.toolbar-group` / `.time-range-bar` containing `.toolbar-btn` / `.time-range-btn`, with `.active` and `aria-pressed`.
- **Search**: `.toolbar-search` label with an SVG icon and an `input[type=search]`.
- **Forms**: `.form-group`, `.form-label`, `.form-input` / `.form-select` / `.form-textarea`, `.form-hint`, `.form-grid-2`, `.input-prefix` (address prefix), `.toggle-pill` (checkbox chip), `.node-select-pill` and `.form-fieldset`.
- **Tables**: `.table-responsive` (a positioned scroll container) holding a `.table`. Helpers: `.table-actions`, `.table-link`, `.table-empty`.
- **Empty states**: `.empty-state` with `.empty-state-title`, a sentence, and the action that fixes it.

### In-place editing
Agent rename and short notes are edited inline in the detail header. Read and edit states are toggled with the `hidden` attribute. `.editable-title` is focusable (`role="button"`, `tabindex="0"`, Enter starts editing), Enter saves and Escape cancels. Long notes (> 40 characters or multiline) appear in an expanded notes card under the metric tiles.

### Dialogs
All modals are native `<dialog class="modal">` elements opened with `showModal()`, which gives the focus trap, Esc to close and `::backdrop` for free.

- Structure: `.modal-header` (`.modal-title`, `.modal-subtitle`, `{{template "modal_close"}}`), then `.modal-body` (scrolls), then `.modal-footer`. Inside a `<form>`, the form wraps the body and footer.
- Open and close from markup with `data-open-modal="id"` and `data-close-modal` (closes the enclosing dialog). From JS, use `CertainStatsModal.open(id)` and `CertainStatsModal.close(id)`.
- A click on the backdrop closes the dialog. Use `.modal-lg` for wide dialogs.
- Current dialogs:
  - agent dialogs: add agent, notes, reinstall, uninstall;
  - alert dialogs: new and edit rule, new and edit target, template variables;
  - delete status page;
  - confirm (shared, see below).
- **Confirmations.** Use the shared `{{template "confirm_modal"}}` partial instead of `window.confirm()`.
  - A form opts in with `data-confirm="message"`, plus optional `data-confirm-title`, `data-confirm-label`, `data-confirm-danger` and `data-confirm-match` (text the user must type before the button enables; use it for deletes).
  - `data-confirm-flash` is shown as a toast on the page the submit lands on (stored in `sessionStorage`).
  - From JS, `CertainStatsModal.confirm({title, message, label, danger, match})` returns a `Promise<boolean>`.
  - Currently used on the Management page. Other pages still use `confirm()` and can move over.

### Credentials
- Secrets (agent tokens) render masked through the `maskSecret` template func: 12 dots plus the last four characters.
- The real value lives in `data-secret` on the `<code class="secret">` element. A Show button swaps it in, and Copy always copies the full value.
- Masking is for screen-sharing and shoulder-surfing, not access control: the value is still in the page source of a signed-in page.
- Long values such as SSH keys and agent IDs are shortened with `middleTrim` and carry the full value in `title`.

### Theme toggle
The `{{template "theme_toggle"}}` icon button cycles **system → light → dark** and stores the preference in `localStorage.certainstats_theme`.

- The `{{template "theme_bootstrap"}}` inline script resolves the preference to `data-theme="light|dark"` before first paint, so there is no flash. It also sets `data-theme-pref`.
- When the preference is `system`, a `matchMedia` listener follows OS changes.
- Every change dispatches `certainstats_theme_change`, which charts and sparklines listen for to redraw.

## JavaScript modules

Dependency-free modules loaded with `<script>` tags carrying SRI hashes. The telemetry, chart, network monitor, Agent Hub and public dashboard scripts are generated bundles: edit their sources under `web/src/js`, as described in [frontend JavaScript build](frontend-js-build.md).

```mermaid
flowchart LR
    subgraph Core Helpers
        Telemetry[CertainStatsTelemetry + CertainStatsModal]
        ChartEngine[CertainStatsChart]
    end
    subgraph App Modules
        Admin[CertainStatsAdminAgents]
        Public[CertainStatsPublicDashboard]
        Provision[CertainStatsProvisionRenderer]
        Alerts[CertainStatsAdminAlerts]
        Incidents[CertainStatsIncidentHistory]
        DashEdit[CertainStatsDashboardEdit]
        Network[CertainStatsNetworkMonitors]
        Collectors[CertainStatsCollectors]
        Types[CrowdSec and Knot DNS pages]
    end

    Telemetry --> Admin
    Telemetry --> Public
    Telemetry --> Alerts
    ChartEngine --> Admin
    ChartEngine --> Public
    Provision --> Admin
    Network --> Admin
    Collectors --> Types
```

### `CertainStatsTelemetry` ([`telemetry.js`])
Source: [`web/src/js/components/telemetry/`](../../web/src/js/components/telemetry)

Shared by the panel and public pages; it contains only what both use.

- **WebSocket feed** for `/api/ws` and `/api/public/ws/{id}`, with reconnection. `onFeedEvent` lets admin code follow connection and frame events.
- **In-page router (`initRouter`)** maps `/{agent_id}` and `/{slug}/{pub_id}` to view swaps. Callers pass their own `reservedSegments`.
- **`renderClusterStats`** updates the hero figures, the public header status and the public status banner. **`renderPartitions`** renders the storage partition cards.
- **Floating tooltips** on meters and **toasts**.

Admin-only members are added to the same global by `admin_core` ([`web/src/js/components/admin_core/`](../../web/src/js/components/admin_core)), which only signed-in panel pages load:
- **Time range picker (`initCustomTimePicker`)**: quick ranges plus a custom start and end dropdown with `aria-expanded`.
- **Filter menu (`createFilterMenu`, `renderFilterChips`)**: the "+ Filter" popover (field, optional is / is not, value) and the removable chips of active filters, from `partials/filters/menu.html` (`filter_menu`, `filter_chips`). Each page passes its own fields and keeps the filters in its own view; `exclude: false` hides is / is not. CrowdSec and Knot DNS use it.
- **`CertainStatsModal`**: open/close, `data-confirm` forms, and the delegated `data-open-modal`/`data-close-modal` handlers. Also the reinstall and uninstall dialogs.
- **Network pulses** (`subscribeNetwork`, `getNetworkFeedState`) and `drawSparkline` on `CertainStatsChart`.

The public range bar (`initTimeRangeBar`) belongs to the public dashboard bundle and clamps ranges to `MaxDays`.

### `CertainStatsChart` ([`chart.js`])
Source: [`web/src/js/components/chart/`](../../web/src/js/components/chart)

- **Colors from tokens**: grid, axis text, tooltip card, downtime band and drag selection all read CSS variables, and series colors go through `resolveColor`.
- **Style**:
  - smooth curves (horizontal midpoint bezier, so no overshoot) with a 2px stroke and soft gradient area;
  - a marked latest point (a dot with a ring in the surface color);
  - offline gaps drawn as a red band labelled "Offline 42m".
- **Fitted y-axis gutter**: the left padding grows to fit the widest label.
- **Drag to zoom**, a **crosshair tooltip**, and **High-DPI** rendering.
- **`drawSparkline(canvas, points, opts)`**: axis-less trend line for Agent Hub cards and monitor rows, split at data gaps. Admin-only, so `admin_core` adds it.
- **Theme**: `initThemeToggle` handles the three-way preference described under [theme toggle](#theme-toggle).

### `CertainStatsAdminAgents` ([`admin_agents.js`])
Source: [`web/src/js/pages/admin_agents/`](../../web/src/js/pages/admin_agents)

- Switches between card and table views (persisted), and filters by name, CPU or ID.
- Applies live WebSocket snapshots to cards, table rows and the open detail view. Element IDs are the contract between templates and this script, so keep them stable.
- Loads 24h CPU sparklines (`/api/metrics?…&hours=24`), staggered per card.
- Re-renders the headline and offline card washes after each metadata sync.

### `CertainStatsPublicDashboard` ([`public_dashboard.js`])
Source: [`web/src/js/pages/public_dashboard/`](../../web/src/js/pages/public_dashboard)

- Status rows (the default view) or cards, persisted per visitor.
- Strictly respects `AllowedFeatures` and `AllowedMetrics`. Templates use `hasFeature` and `hasMetric`, and scripts check `pubAllowedMetrics`.
- Public agent detail is rendered in place at `/dashboard/{slug}/{pub_id}` with the same structure as the admin detail, minus admin actions.

### `CertainStatsProvisionRenderer` ([`provision_renderer.js`])
Agent onboarding, install and uninstall instructions, with copy buttons. Driver choice is a `radiogroup` of `.driver-select-card` buttons.

### `CertainStatsDashboardEdit` ([`dashboard_edit.js`])
Status page editor:
- server selection and aliases;
- drag-to-reorder, plus up and down buttons for keyboard users (`data-move`);
- order is submitted as repeated `agents_order` hidden inputs with the `is_dragged` flag.

### `CertainStatsNetworkMonitors` ([`network_monitors.js`])
Source: [`web/src/js/components/network_monitors/`](../../web/src/js/components/network_monitors)

The Network page and the network section of agent detail: grouped monitor list, response-time and failed-check graphs, and the monitor and group editors. Behaviour is described in [network monitoring](../operate/network-monitoring.md); internals in [network monitoring internals](network-monitoring.md).

### `CertainStatsAdminAlerts` ([`admin_alerts.js`]) and `CertainStatsIncidentHistory` ([`incident_history.js`])
Hand-written admin scripts for the Alerts page: rule and target dialogs (condition fields change with the trigger type, and collector conditions show a **Watch** list), and the incident history table.

### `CertainStatsCollectors` ([`collectors.js`])
Source: [`web/src/js/components/collectors/`](../../web/src/js/components/collectors)

The Collectors page: collector types (each leading to its page), the collector list, add, rename, token reset, removal, and turning types on or off. Collector type pages load it for the data only and follow changes through `CertainStatsCollectors.subscribe`.

### CrowdSec and Knot DNS pages ([`crowdsec.js`], [`knot.js`])
Sources: [`web/src/js/pages/crowdsec/`](../../web/src/js/pages/crowdsec), [`web/src/js/pages/knot/`](../../web/src/js/pages/knot)

Each reads its data from `/api/collectors/<type>` and uses the shared filter menu and time picker from `admin_core`. See [CrowdSec](../operate/collectors/crowdsec.md#the-page) and [Knot DNS](../operate/collectors/knot.md#the-page).

## Templates and routes

### Template tree

```
web/templates/
├── layout/
│   ├── base.html              # Admin shell: skip link, navbar, mobile subnav, flash, footer
│   ├── public_base.html       # Public shell: page title + live status, theme toggle
│   └── auth_base.html         # Sign-in and setup shell
├── partials/
│   ├── common/
│   │   ├── shell.html         # theme_bootstrap, logo_mark, theme_toggle, modal_close
│   │   ├── confirm_modal.html
│   │   ├── reinstall_modal.html
│   │   └── uninstall_modal.html
│   ├── agents/                # headline, cluster_cards (hero stats), toolbar, grid/list views,
│   │                          # inpage_detail, network_monitors, provision_modal, notes_modal
│   ├── public/                # cluster_cards, toolbar, grid_view (cards), list_view (status rows),
│   │                          # public_inpage_detail
│   ├── alerts/                # rules/targets/history tables, incidents banner, subjects, dialogs
│   ├── collectors/            # add_modal: add a collector, setup steps
│   ├── crowdsec/              # hero, components, filters, panels, table, ip_view
│   ├── filters/               # menu: shared "+ Filter" menu and chips
│   ├── knot/                  # hero, toolbar, checks, panels
│   └── settings/              # password_form, sessions_table
├── agents_list.html           # Agent Hub, in-page agent detail, Network page
├── agent_management.html      # Agent credentials (tokens, SSH keys, resets)
├── dashboards_list.html       # Status pages list
├── dashboard_edit.html        # Status page editor
├── alerts_list.html           # Alert rules, targets, incident history
├── collectors.html            # Collector types and the collector list
├── collector_types/           # one page per collector type, served at /collectors/<name>
│   ├── crowdsec.html          # CrowdSec alerts
│   └── knot.html              # Knot DNS traffic, zones and dnstap
├── settings.html              # Password and signed-in devices
├── login.html
├── setup.html
└── public_dashboard.html      # Public status page + in-page public agent detail
```

### Application shell
- **Brand**: SVG logo mark (a pulse line in a rounded teal square) with the "CertainStats" wordmark.
- **Navigation**: Agent Hub, Dashboards, Network, Collectors, Alerts, Management, Settings. Collector type pages (CrowdSec, Knot DNS) are reached from Collectors and keep it active. The active link has `aria-current="page"` and a teal underline. On mobile the links move to a horizontally scrolling subnav.
- **Right side**: theme toggle and Log out.
- **Navbar**: translucent and sticky. It gains a bottom border once the page scrolls (`.is-scrolled`).
- **Footer**: copyright and server render time ("Rendered in 0.35ms").

### Routes

| URL Route | Template | JavaScript Entry | Key Functionality |
|---|---|---|---|
| `/` | `agents_list.html` | `admin_agents.js` | Agent Hub: headline, fleet figures, cards/table, search, sparklines |
| `/{agent_id}` | `agents_list.html` | `admin_agents.js` | In-page agent detail: tiles, storage, history charts, notes |
| `/network-monitors` | `agents_list.html` | `admin_agents.js`, `network_monitors.js` | Network page: graphs and grouped monitor list. See [network monitoring](../operate/network-monitoring.md) |
| `/agents/management` | `agent_management.html` | `agent_management.js`, `provision_renderer.js` | Expandable credential rows: masked token, SSH key, rename, install, uninstall, resets, delete; search |
| `/dashboards` | `dashboards_list.html` | `admin_modals.js` | Status pages list |
| `/dashboards/new`, `/dashboards/{id}` | `dashboard_edit.html` | `dashboard_edit.js` | Address, visibility rules, servers, order |
| `/collectors` | `collectors.html` | `collectors.js`, `provision_renderer.js` | Collector types (each leading to its page), collector list, add, install, token reset, removal. See [collectors](../operate/collectors/README.md) |
| `/collectors/crowdsec` | `collector_types/crowdsec.html` | `crowdsec.js`, `collectors.js` | CrowdSec alerts: headline, filters, timeline, breakdowns, alert table, IP view; Collectors dialog. `/crowdsec` redirects here. See [CrowdSec](../operate/collectors/crowdsec.md) |
| `/collectors/knot` | `collector_types/knot.html` | `knot.js` | Knot DNS: headline, counter charts, dnstap drill-down, zones across servers. `/knot` redirects here. See [Knot DNS](../operate/collectors/knot.md) |
| `/alerts` | `alerts_list.html` | `admin_alerts.js`, `incident_history.js` | Rules, targets, incident history |
| `/settings` | `settings.html` | `admin_modals.js` | Password change, signed-in devices |
| `/login`, `/first-time-setup` | `login.html`, `setup.html` | `setup.js` (setup only) | Sign in, initial administrator account |
| `/dashboard/{slug}` | `public_dashboard.html` | `public_dashboard.js` | Public status page |
| `/dashboard/{slug}/{pub_id}` | `public_dashboard.html` | `public_dashboard.js` | Public agent detail |

## Performance and assets

### Server rendering
- Templates are pre-parsed on startup via `NewTemplateRenderer` in `renderer.go` into memory.
- Typical page render time is **< 0.50 ms**, shown in the footer via `PageData.RenderTime()`.

### Embedding and minification
- Static assets (CSS, JS, fonts) and templates are embedded into the Go binary using `embed.FS` in `web/embed.go`.
- Assets are grouped by audience: `shared/`, `admin/`, `public/` and `auth/` under `web/static`. The public router serves only `shared` and `public`; the panel serves `shared` and `auth` to anyone and `admin` only with a session. Each layout (`base.html`, `public_base.html`, `auth_base.html`) loads `shared/css/base.css` plus its own area stylesheet. See [frontend JavaScript build](frontend-js-build.md).
- The larger scripts are bundled from `web/src/js` by `tools/js-bundle` ahead of time and checked in, so the Go build only sees ordinary files in `web/static/<area>/js`.
- CSS and JS are minified at startup by `internal/minify` and served under content-hashed names with SRI. Fonts pass through unchanged and are referenced by stable paths (`/static/shared/fonts/*.woff2`).
- The CSS minifier keeps whitespace that changes meaning: around `(` in media queries, after `)` in selectors such as `:not(…) .x`, and inside `calc()` and `color-mix()`.

### Output caching
- Public status pages are cached in `DashboardHTMLCache` (`internal/context/key.go`) for 60 seconds, within the shared 128 MiB response budget, and invalidated when the dashboard's configuration changes. See [caching standards](caching-standards.md).
