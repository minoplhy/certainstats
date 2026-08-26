# CertainStats — Frontend Design Document

> **Version:** 2.0 · **Date:** 2026-07-20 · **Author:** Minoplhy

---

## 1. Design Philosophy

> **"Premium. Dense. Real-time."**

CertainStats UI is inspired by best-in-class ops dashboards — Datadog, Fly.io, Railway — and is designed around three hard rules:

1. **Status at a glance.** Any operator should know cluster health within 1 second of opening the panel.
2. **Dark-first, light-supported.** Dark mode is the primary design canvas. Light theme is a first-class inversion with its own tuned token values — not an afterthought.
3. **Zero noise.** Every element earns its place. No decorative chrome, no wasted padding.
4. **Micro-motion everywhere.** Live data updates, hover lifts, popover expansions, bar width changes all animate. A static UI feels dead.
5. **Monospace for data.** All metric values, IDs, timestamps, and code use `JetBrains Mono`. Human labels use `Outfit` (display headings) or `Inter` (body).

---

## 2. Design Tokens

All tokens live in [`frontend-admin/src/app/globals.css`](../frontend-admin/src/app/globals.css) on `:root` (dark default) and `[data-theme='light']`.

### 2.1 Color Palette

#### Dark Theme (default — `:root`)

```css
/* Backgrounds */
--bg-primary:   #0a0a0c;           /* Page background */
--bg-secondary: #121318;           /* Cards, panels */
--bg-panel:     rgba(18,19,24,.7); /* Glassmorphic overlays */

/* Borders */
--border-color: rgba(255,255,255,0.06);  /* Resting border */
--border-hover: rgba(255,255,255,0.15);  /* Hovered border */

/* Text */
--text-primary:   #ffffff;
--text-secondary: #cbd5e1;
--text-muted:     #8e909a;

/* Accent — Indigo */
--accent-primary:   #6366f1;             /* Buttons, active tabs, highlights */
--accent-secondary: #8b5cf6;             /* Gradient endpoint (violet) */
--accent-glow:      rgba(99,102,241,0.4);/* Glow / box-shadow for accent elements */

/* Semantic status */
--status-online:  #10b981;  /* Emerald green */
--status-offline: #ef4444;  /* Red */
--status-warning: #f59e0b;  /* Amber */

/* Glass & misc */
--glass-bg:    rgba(18,19,24,0.45);
--glass-border:rgba(255,255,255,0.08);
--bar-bg:      rgba(255,255,255,0.05);  /* UsageBar empty track */
--card-shadow: 0 4px 20px rgba(0,0,0,0.4);
```

> **Why indigo?** `#6366f1` (Tailwind `indigo-500`) reads authoritative and calm — not alarming like red/orange, not cold like pure blue. Appropriate for a monitoring tool where the goal is clarity, not anxiety.

#### Light Theme (`[data-theme='light']`)

```css
--bg-primary:     #f8f9fa;
--bg-secondary:   #ffffff;
--accent-primary: #4f46e5;            /* Deeper indigo — better contrast on white */
--accent-glow:    rgba(79,70,229,0.2);
--bar-bg:         rgba(0,0,0,0.04);
--card-shadow:    0 4px 20px rgba(0,0,0,0.08);
```

Everything else (text, border, status) has light-mode overrides tuned so contrast ratios stay WCAG AA compliant.

### 2.2 Semantic Spec Tile Palette

Hardware spec tiles in the Agent Detail hero use a separate set of per-category tokens:

| Prefix | Color family | Tile type |
|---|---|---|
| `--spec-indigo-*` | Indigo | CPU model, cores |
| `--spec-green-*` | Emerald | RAM, swap |
| `--spec-blue-*` | Sky | Network info |
| `--spec-amber-*` | Amber | Disk, storage |

Each prefix has three variants: `-bg` (background), `-border` (border), `-color` (icon/label tint).

### 2.3 Metric Color Map

These colors are **semantically fixed** — the same metric always uses the same color across all bars, chart series, and legend dots:

| Metric | Color | Hex |
|---|---|---|
| CPU Used | Blue | `#3b82f6` |
| CPU IO Wait | Orange | `#fb923c` |
| CPU Steal | Red | `#ef4444` |
| RAM Used | Teal | `#14b8a6` |
| Swap Used | Slate | `#4b5563` |
| Disk Used | Purple | `#a855f7` |
| Network RX | Dark blue | `#1e40af` |
| Network TX | Dark purple | `#7e22ce` |
| Disk Read | Amber | `#f59e0b` |
| Disk Write | Red | `#ef4444` |

### 2.4 Typography

```css
--font-sans:    'Inter', system-ui, sans-serif;       /* Body, labels, nav */
--font-display: 'Outfit', system-ui, sans-serif;      /* Page titles, card headings */
--font-mono:    'JetBrains Mono', monospace;          /* All metric values, IDs, code */
```

Fonts load via `<link>` in `index.html` (not CSS `@import`) — avoids render-blocking.

#### Type Scale

| Usage | Size | Weight | Font |
|---|---|---|---|
| Page title (`h1`) | 28px | 700 | display |
| Card heading | 18px | 800 | display |
| Section label | 9–11px | 700–800 | sans, ALL CAPS, `letter-spacing: 0.05–0.12em` |
| Body / table cells | 13px | 400 | sans |
| Sidebar items | 15px | 500 | sans |
| Metric values | 13–16px | 600 | **mono** |
| Badge / micro-label | 11px | 700 | sans or mono |

### 2.5 Transitions

```css
--transition-fast:   0.15s ease;
--transition-normal: 0.3s ease;
```

Never hardcode transition durations — always reference these tokens. This lets theme switches, debug overrides, and reduced-motion media queries apply globally.

---

## 3. Global Body & Ambient Glow

The `<body>` has a two-point radial gradient to create an ambient indigo halo behind all content:

```css
body {
  background: var(--bg-primary);
  background-image:
    radial-gradient(circle at 10% 40%, var(--accent-glow), transparent 30%),
    radial-gradient(circle at 90% 10%, var(--accent-glow), transparent 30%);
}
```

This makes the dark background feel deep rather than flat — a subtle studio-lighting effect. In light mode it's barely perceptible but present. **Do not remove this.**

---

## 4. Component Library

### 4.1 `.glass-panel`

Glassmorphic overlay panel — floating menus, profile dropdown, modal headers.

```css
.glass-panel {
  background:       var(--glass-bg);
  backdrop-filter:  blur(16px);
  -webkit-backdrop-filter: blur(16px);
  border:           1px solid var(--glass-border);
  border-radius:    12px;
  box-shadow:       0 4px 24px rgba(0,0,0,0.2);
}
```

> ⚠️ On mobile (`max-width: 768px`), `backdrop-filter` is **disabled** on modals for GPU performance. Replace with a solid `rgba(18,19,24,0.95)` background instead.

### 4.2 `.card`

Standard content card. The base for agent cards, alert cards, and stat tiles.

```css
.card {
  background:    var(--bg-secondary);
  border:        1px solid var(--border-color);
  border-radius: 12px;
  padding:       24px;
  transition:    var(--transition-normal);
}
```

**Hover state** (non-touch devices):
- `border-color → var(--border-hover)`
- `box-shadow → 0 8px 32px rgba(0,0,0,0.15)`

Mobile override: `padding: 16px`.

### 4.3 `.btn-primary`

Gradient indigo→violet CTA button with glow.

```css
background:    linear-gradient(135deg, var(--accent-primary), var(--accent-secondary));
box-shadow:    0 4px 12px var(--accent-glow);
border-radius: 8px;
padding:       10px 20px;
font-weight:   600;
font-size:     13px;
color:         #fff;
border:        none;
```

| State | Transform | Shadow |
|---|---|---|
| Hover | `translateY(-1px)` | expanded glow |
| Active | `translateY(1px)` | reduced glow |
| Disabled | none | `opacity: 0.5` |

Agent Hub CTA variant: `padding: 12px 28px`, `border-radius: 14px`, `font-size: 14px`, `font-weight: 800`.

### 4.4 `.btn-secondary`

Ghost/outline button with semi-transparent background.

```css
background:    rgba(255,255,255,0.05);
border:        1px solid var(--border-color);
border-radius: 8px;
padding:       10px 20px;
font-weight:   600;
font-size:     13px;
```

Hover: `background → rgba(255,255,255,0.08)`, `border-color → var(--border-hover)`.

### 4.5 `.btn-icon-only`

Square icon-only button, `36×36px`, `border-radius: 10px`.

**`.danger` variant:**
- Default: `color: var(--status-offline)`, red-tinted border
- Hover: `box-shadow: 0 0 12px rgba(239,68,68,0.15)`

### 4.6 `.input-field`

```css
background:    rgba(0,0,0,0.2);
border:        1px solid var(--border-color);
border-radius: 8px;
padding:       10px 14px;
font-size:     13px;
font-family:   var(--font-sans);
color:         var(--text-primary);
```

Focus ring: `border-color → var(--accent-primary)` + `box-shadow: 0 0 0 2px var(--accent-glow)`.

### 4.7 `.badge`

Inline status chip: `padding: 4px 8px`, `border-radius: 6px`, `font-size: 11px`, uppercase, `letter-spacing: 0.5px`.

| Variant | Background | Text |
|---|---|---|
| `.badge-online` | `rgba(16,185,129,0.1)` | `--status-online` |
| `.badge-offline` | `rgba(239,68,68,0.1)` | `--status-offline` |

### 4.8 `.status-pill`

Rounded pill with an animated inner dot. Used in the Agent Detail hero.

```css
.status-pill           { border-radius: 20px; padding: 4px 10px; font-size: 10px; font-weight: 800; }
.status-pill.online    { background: rgba(16,185,129,0.08); color: var(--status-online); }
.status-pill.offline   { background: rgba(239,68,68,0.08);  color: var(--status-offline); }
```

The inner `.status-dot-pulse` for `.online` runs `pulse-glow-micro` (scale `0.9→1.2`, opacity `0.8→1`, 2s infinite).

### 4.9 `.status-dot-online` / `.status-dot-offline`

8×8px filled circle. Online variant: continuous `pulse-glow` keyframe — a green box-shadow expanding `0→6px→0` over 2s, infinite. Appears in all agent list/card rows.

### 4.10 `.type-badge`

Agent type chip (e.g., BESZEL / LTSTATS / HETRIXTOOLS). 9px, `font-weight: 900`, indigo-tinted background, uppercase.

### 4.11 Toggle Switch

Custom toggle (not native `<input type="checkbox">`):
- Pill: `40×24px`, `border-radius: 12px`
- Thumb: `18×18px` circle
- Enabled: `background: var(--status-online)`, thumb slides right
- Disabled: semi-transparent border, dark background
- Transition: `var(--transition-fast)` on all properties

Used on every alert card.

### 4.12 Modals

Two distinct implementations depending on context:

**Full-screen overlay** (`.modal-overlay` + `.modal-content`):
- Overlay: `rgba(10,10,12,0.85)` + `backdrop-filter: blur(12px)`
- Content animates in with `modalScale` keyframe (scale `0.95→1.0`, 0.3s spring)
- Closes on overlay click via `document.addEventListener('mousedown', clickOutside)`

**Inline portal modal** (`.modal-backdrop`):
- `position: absolute; z-index: -1` inside a fixed wrapper
- Used when the modal needs to stay anchored to its trigger (e.g., reinstall instructions)
- Mobile: blur disabled, background `rgba(10,10,12,0.95)` solid

### 4.13 Action Menu (Popover)

Portal-rendered via `ReactDOM.createPortal(_, document.body)`. Key behaviour:
- **Smart positioning:** measures `rect.bottom` vs `menuHeight` — opens upward if not enough space below
- **Animation:** `menuExpand` keyframe — scale `0.95→1`, `translateY(-4px→0)`, 0.12s `cubic-bezier(0.16,1,0.3,1)`, `transform-origin: top right`
- **Dismiss:** click outside via `document.addEventListener('mousedown', handler, true)` (capture phase to beat portal events)

```css
@keyframes menuExpand {
  from { opacity: 0; transform: scale(0.95) translateY(-4px); }
  to   { opacity: 1; transform: scale(1)    translateY(0); }
}
```

---

## 5. Core Shared Components

### 5.1 `UsageBar`

**File:** [`frontend-admin/src/lib/UsageBar.tsx`](../frontend-admin/src/lib/UsageBar.tsx)

The primary data visualization primitive — a segmented horizontal bar with an optional legend and portal tooltip.

#### Two Modes

| Mode | Height | Legend | Tooltip | Usage |
|---|---|---|---|---|
| **Detailed** (default) | 8px | 2-col grid below | — | Agent grid cards, public dashboard |
| **Compact** (`compact={true}`) | 4px | None | Portal on hover | Sidebar mini-cards, list-view cells |

#### Detailed Mode Structure

```
Label (sans, 11px, muted)              Value (mono, 11px, primary)
┌────────────────────────────────────────────────────────────┐
│ ████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░│  ← 8px bar, border-radius: 4px
└────────────────────────────────────────────────────────────┘
  ● Used  62.0%    ● IO Wait  3.2%    ← legend, 2-col grid, 10px dots
```

Each segment's width is `(value / total) * 100%`, transitions with `width 0.5s ease`.

#### Compact Mode + Tooltip

- Bar: `4px` height, `border-radius: 2px`
- No label shown inline
- Hover → portal tooltip (`document.body`) showing segment name + value per row, glassmorphic background

#### Segment Color Assignment

Colors are passed in as a `segments` prop array; the consuming component hardcodes the metric → color mapping per the [Metric Color Map](#23-metric-color-map).

### 5.2 `TelemetryChart`

**File:** [`frontend-admin/src/app/agent/TelemetryChart.tsx`](../frontend-admin/src/app/agent/TelemetryChart.tsx)

Recharts wrapper. Renders either `AreaChart` (when any series has `fill: true`) or `LineChart`.

#### Key Design Decisions

| Feature | Implementation |
|---|---|
| **No animation** | `isAnimationActive={false}` — prevents jank on live-updating data |
| **Downtime shading** | Red `<ReferenceArea>` zones where data gap > 2.5× normal poll interval; `fill: rgba(239,68,68,0.22)`, dashed red stroke |
| **Drag-to-zoom** | `onMouseDown/Move/Up` + `onTouch*` handlers — drag draws a `<ReferenceArea>`, releases call `onZoom(start, end)` |
| **Dynamic Y-axis** | `yMax = Math.max(maxValueProp, computedPeakFromData)` — never hardcoded |
| **Time axis format** | Adaptive: `HH:mm` (<24h), `D MMM` (<1y), `D MMM YYYY` (older) |
| **Tooltip** | Glassmorphic via Recharts CSS overrides in `globals.css`; mono font, blurred backdrop |

#### Recharts CSS Overrides in `globals.css`

```css
.recharts-tooltip-wrapper .recharts-default-tooltip {
  background: var(--glass-bg) !important;
  backdrop-filter: blur(12px) !important;
  border: 1px solid var(--glass-border) !important;
  border-radius: 8px !important;
  font-family: var(--font-mono) !important;
  font-size: 12px !important;
}
```

### 5.3 `TotalTelemetryPanel`

**File:** [`frontend-admin/src/lib/TotalTelemetryPanel.tsx`](../frontend-admin/src/lib/TotalTelemetryPanel.tsx)

Four aggregate stat cards in a `auto-fit minmax(260px, 1fr)` responsive grid, shown above the agent list on the Hub and on the Public Dashboard.

| Card | Icon | Accent stripe | Metric |
|---|---|---|---|
| Total Bandwidth | `swap_vertical_circle` | Indigo | Live aggregate RX + TX bps |
| Total Disk I/O | `storage` | Orange `#fb923c` | Live aggregate Read + Write bps |
| Total Traffic | `cloud_sync` | Teal `#14b8a6` | Cumulative RX + TX bytes |
| Total Disk Read/Write | `database` | Red `#ef4444` | Cumulative Read + Write bytes |

Each card: `border-radius: 16px`, `padding: 16px`, a colored 3px gradient bottom bar (the accent stripe), value as `<number><unit>` with unit in 10px sans at 60% opacity.

On the Public Dashboard, filtered by `allowedMetrics` prop — only whitelisted cards render.

---

## 6. Layout System

### 6.1 App Shell

```
┌──────────────────────────────────────────────┐
│  <PanelNav>     sticky top 0, height 64px    │
├────────────┬─────────────────────────────────┤
│  .sidebar  │  <main class="main-panel">      │
│  -panel    │   overflow-y: auto              │
│            │   flex: 1                       │
│            │   padding: 32px (desktop)       │
│            │            12px (mobile)        │
└────────────┴─────────────────────────────────┘
```

The sidebar is **always rendered** on main routes (`/`, `/agent/:id`, `/dashboards`, `/alerts`). It is **not rendered** on full-page form routes: `/dashboards/create`, `/dashboards/edit`, `/alerts/create`, `/alerts/edit`, `/targets/create`, `/targets/edit`.

### 6.2 Sidebar Behaviour

- **Desktop expanded:** icon + label, fixed width
- **Desktop collapsed:** icon strip only, toggled by a chevron button
- **Mobile (≤768px):** fixed-position overlay, 280px wide, full viewport height, `z-index: 300`, heavy box-shadow. Triggered by hamburger in navbar. Tap outside to close.

### 6.3 Content Max-Widths

| Page | Max-width |
|---|---|
| Agent Hub (grid) | `1200px` |
| Agent Detail | `1200px` |
| Alerts Panel | `1000px` |
| Management View | `1400px` |

All centered via `margin: 0 auto` inside `.panel-content`.

### 6.4 Responsive Utility Classes

| Class | Behavior at `≤768px` |
|---|---|
| `.mobile-hide` | `display: none` |
| `.mobile-only` | visible (hidden on desktop) |
| `.mobile-stack` | `flex-direction: column; gap: 16px` |
| `.mobile-full` | `width: 100%` |
| `.mobile-grid-1` | `grid-template-columns: 1fr` |
| `.panel-content` | `padding: 12px` (vs `32px` desktop) |
| `.search-bar-container` | stacks vertically, full-width |

---

## 7. Navigation Bar (`PanelNav`)

Height: `64px`, `position: sticky; top: 0; z-index: 50`, `background: var(--bg-primary)`, `border-bottom: 1px solid var(--border-color)`.

Inner wrapper: `max-width: 1200px; margin: 0 auto; display: flex; justify-content: space-between; align-items: center`.

### Left side

```
[☰ hamburger (mobile only)] [◈ logo] [CertainStats] [Agents] [Dashboards] [Alerts]
```

- **Logo:** 32×32px box, `border-radius: 8px`, `linear-gradient(135deg, #6366f1, #8b5cf6)`, white `hub` Material Symbol, `box-shadow: 0 4px 12px rgba(99,102,241,0.3)`
- **"CertainStats":** 18px, weight 800, `letter-spacing: -0.02em`, `font-family: var(--font-display)`
- **Nav links:** 13px, weight 500 resting, weight 600 + `border-bottom: 2px solid var(--accent-primary)` when active

### Right side

```
[☾/☀ theme toggle] [│] [⊙ profile]
```

- **Theme toggle:** icon-only button, cycles `data-theme` attribute on `<html>`, persists to `localStorage`
- **Profile button:** opens a 220px `.glass-panel` dropdown: "Agents Management", "User Management", divider, "Sign Out" (red text)

### Mobile

Nav links are hidden; a secondary `48px` tab bar appears below the navbar with icon + label tabs for Agents / Dashboards / Alerts.

---

## 8. Page Designs

### 8.1 Agent Hub (`AgentView`)

**Route:** `/`

```
[Title: Agent Hub]  [╔ N Nodes ╗]  ─────────────  [⊕ Add Agent]
[🔍 Search by nickname, agent or OS...]  [⊞ density] [≡ view toggle]
[TotalTelemetryPanel: 4 aggregate stat cards]
[Agent grid or list]
```

#### Agent Grid Card (`border-radius: 24px`)

```
[●] [nickname]                      [⋮]   ← status dot (animated glow) + action menu
[cpu model · OS version]                  ← shown in Detailed mode only
CPU  ████████░░░░░░░░  62%
     ░░░░░░░░░░░░░░░░   0%          ← IO Wait row (separate segment)
RAM  ████████████░░░░   8.1 GB
     ░░░░░░░░░░░░░░░░   0 B         ← Swap row
DSK  ████░░░░░░░░░░░░  210 GB
NET  ██░░░░░░░░░░░░░░   2.4 MB/s
     ████░░░░░░░░░░░░   1.1 MB/s
IO   ████░░░░░░░░░░░░  840 KB/s
─────────────────────────────────
⏱ 42d 7h 14m                DNS  ← mono font, muted
```

**Hover:** `translateY(-4px)` + `border-color → var(--accent-primary)` + enhanced box-shadow.
**Offline card:** bars replaced with dashed placeholder skeletons, "OFFLINE" status, "Last seen Xm ago".
**`pulse-flash` class** applied for 400ms when a new live metric arrives via WebSocket.

#### Grid Density Modes

| Mode | `grid-template-columns` | Padding | Legend | CPU/OS info |
|---|---|---|---|---|
| Detailed | `minmax(360px, 1fr)` | 28px | ✓ shown | ✓ shown |
| Simplified | `minmax(320px, 1fr)` | 18px | ✗ hidden | ✗ hidden |

Bars in Simplified are `4px` height (compact mode).

#### List View

Table columns: **Agent** (status dot + name) · **Current Load** (compact UsageBars) · **CPU** · **OS** · **RAM** · **Disk** · **Type** · **Actions**.

#### Empty State

Center-aligned `cloud_off` icon (64px, `--text-muted`), "No agents found" label, "Add your first agent" CTA button.

---

### 8.2 Agent Detail (`AgentDetailView`)

**Route:** `/agent/:id`

Same shell (sidebar still visible). Main content switches to single-agent telemetry.

#### Hero Block (`.detail-hero`)

Radial gradient tinted background. `::before` pseudo-element adds a 1px top-edge light-leak:

```css
.detail-hero::before {
  height: 1px;
  background: linear-gradient(90deg, transparent 0%, rgba(255,255,255,0.15) 50%, transparent 100%);
}
```

Contents:

```
╔ ● ONLINE ╗  ╔ BESZEL ╗     [✎ Rename]  [↺ Reinstall]  [🗑 Terminate]
╚══════════╝  ╚════════╝

Agent Nickname  (click → inline input to rename)
Optional note text (italic, muted)

┌─ CPU MODEL ─┐  ┌─ MEMORY ───┐  ┌─ STORAGE ──┐
│ Xeon E5-2680│  │ 16.0 GB RAM│  │ 2.0 TB     │
└─────────────┘  └────────────┘  └────────────┘
┌─ CORES ─────┐  ┌─ OS ───────┐  ┌─ UPTIME ───┐
│ 14C / 28T   │  │ Linux 6.6  │  │ 42d 7h 14m │
└─────────────┘  └────────────┘  └────────────┘

┌─ ↓ Total RX ─────┐  ┌─ ↑ Total TX ─────┐
│  1.23 TB          │  │   890 GB          │
└───────────────────┘  └───────────────────┘
```

**`.spec-tile-grid`:** `auto-fit minmax(220px, 1fr)`. Each tile: 38×38px icon box + label (9px, uppercase) + value (13px mono). Hover: `translateY(-2px)` + `box-shadow: 0 6px 24px rgba(0,0,0,0.25)`.

**`.odometer-tile`:** dashed border by default, solid on hover. Values in mono with ↓/↑ directional arrows.

Mobile (≤768px): spec-tile grid forces 2 columns → (≤600px) 1 column.

#### Chart Area

Time range selector: segmented control `[ 1h ] [ 6h ] [ 24h ] [ 7d ]`. Active segment: indigo filled, `border-radius: 6px`. Custom range: drag on the chart.

Charts in a `.grid-charts` 2-column grid on desktop, 1-column on mobile. Each ~220px tall via `ResponsiveContainer width="100%"`.

| Chart | Type | Series |
|---|---|---|
| CPU Usage | AreaChart | Used (fill) + IO Wait + Steal |
| RAM Usage | AreaChart | Used (fill) + Swap |
| Disk per mount | LineChart | One line per mount point |
| Network RX/TX | LineChart | RX + TX |
| Disk IO | LineChart | Read + Write |

---

### 8.3 Alerts Panel (`AlertsPanel`)

**Route:** `/alerts`

```
Alerting                              [⊕ Create ▾]
─────────────────────────────────────────────────
[Alert Rules & History]  |  [Preset Targets]
══════════════════════

[⚠ ACTIVE INCIDENTS banner — only shown when firing/failed alerts exist]

CONFIGURED ALERTS
─────────────────────────────────────────────────────────────────
(●) ☁  Node Offline Alert          for 2m    [✎] [🗑]
        agent_down · 4 nodes · via Discord
─────────────────────────────────────────────────────────────────
(●) ⧖  High CPU Alert              for 5m    [✎] [🗑]
        cpu_usage > 85% · 6 nodes · via Webhook
─────────────────────────────────────────────────────────────────
```

**Toggle switch:** `(●)` = enabled (green), `(○)` = disabled (border only).

**Active Incidents banner:** red-bordered `.glass-panel`, warning icon, one row per incident with agent name, alert type, "Go to Node" + "Manage" buttons.

**"Create ▾" button:** dropdown opens to "New Alert Rule" / "New Preset Target".

**History tab:** searchable (debounced 350ms), paginated with smart ellipsis, status filter (All / Firing / Resolved segmented control), sortable columns.

---

### 8.4 Alert Create/Edit Form

**Route:** `/alerts/create`, `/alerts/edit/:id` (sidebar hidden)

```
← Back to Alerts      Create Alert Rule
────────────────────────────────────────

  Alert Nickname
  ┌───────────────────────────────┐
  │  High CPU Production          │
  └───────────────────────────────┘

  Trigger Condition
  ┌────────────────┐  ┌───┐  ┌──────┐  ┌─────────────┐
  │  CPU Usage  ▾  │  │ > │  │  85  │  │  for  5m  ▾ │
  └────────────────┘  └───┘  └──────┘  └─────────────┘
  metric              op     threshold  sustained duration

  Target Nodes                          [🔍 filter...]
  ┌────────────────────────────────────────────────────┐
  │  ☑ web-prod-01   ☑ db-primary   ☐ staging-01      │
  │  ☑ worker-01     ☑ cache-01     ☐ monitor-eu       │
  └────────────────────────────────────────────────────┘

  Notification Action
  ◉ Preset Target   ○ Discord Webhook   ○ Custom Webhook

  ┌──────────────────────────────┐  ┌──────────────────┐
  │  #ops-alerts (Discord)    ▾  │  │  🔔 Send Test    │
  └──────────────────────────────┘  └──────────────────┘

  ☑ Enabled

             [ Cancel ]   [ ◈ Save Alert Rule ]
```

---

### 8.5 Dashboards List & Create/Edit

**Route:** `/dashboards` (list, sidebar shown), `/dashboards/create`, `/dashboards/edit/:id` (sidebar hidden)

**Dashboard list cards:** slug badge, title, agent count, max days, edit + delete controls.

**Create/Edit form:**

```
← Back   Create Public Dashboard
──────────────────────────────────

  Dashboard Title     ┌────────────────────────────────┐
                      │  Production Cluster             │
                      └────────────────────────────────┘

  URL Slug            ┌────────────────────────────────┐
                      │  production                     │
                      └────────────────────────────────┘
  URL: https://panel.example.com/dashboard/production

  Allowed Metrics
  ┌──────────────────────────────────────────────────┐
  │  ☑ CPU  ☑ RAM  ☑ Disk  ☑ Net RX  ☑ Net TX      │
  │  ☐ Disk Read  ☐ Disk Write  ☐ CPU IOWait        │
  └──────────────────────────────────────────────────┘

  Max History (days)    ┌──────┐
                        │  30  │
                        └──────┘

  Agents                                [🔍 filter...]
  ┌──────────────────────────────────────────────────┐
  │  ☑ web-prod-01   Alias: [web-prod  ]  Sort: [1] │
  │  ☑ db-primary    Alias: [database  ]  Sort: [2] │
  │  ☐ staging-01                                    │
  │  ☑ cache-01      Alias: [cache     ]  Sort: [3] │
  └──────────────────────────────────────────────────┘

          [ Cancel ]   [ ◈ Create Dashboard ]
```

---

### 8.6 Management View

**Route:** `/management` (wide table, `max-width: 1400px`)

Tokens shown masked: `cs_a1b2c3d4...e8f9` with a `[⎘ copy]` button. Full token revealed in a success modal after reset.

```
Agents Management              [🔍 Search agents...]
Securely manage tokens and SSH identities.

┌────────────────────────────────────────────────────────────────────┐
│  AGENT          TYPE    AUTH TOKEN               MANAGEMENT    DEL │
│ ──────────────────────────────────────────────────────────────────│
│  web-prod-01   LTSTATS cs_a1b2c3...e8f9  [⎘]   [⊡ Reinstall]    │
│  efc3a0d8-...                                    [↺ Token]    [🗑] │
│ ──────────────────────────────────────────────────────────────────│
│  staging-01    BESZEL  cs_m3n4o5...q7r8  [⎘]   [⊡ Reinstall]    │
│                        ssh-ed25519 AAAA…  [⎘]   [↺ Token]        │
│  2f7e8c1a-...                                    [↺ SSH]      [🗑] │
└────────────────────────────────────────────────────────────────────┘
```

---

### 8.7 Settings

**Route:** `/settings`

Password change form + active sessions table (per-session eject + "Eject All Other Sessions" button).

---

### 8.8 Public Dashboard

**Route:** `/{publicPath}/{slug}` — no auth, no navbar, no sidebar.

```
Production Cluster                    [🔗 Copy Shareable Link]
Public monitoring dashboard · Last updated 12s ago

┌──────────────────┐  ┌──────────────────┐  ┌─────────────────┐
│ ● web-prod-01    │  │ ● db-primary     │  │ ○ staging-01    │
│ CPU  ████░░  62% │  │ CPU  ██░░  18%   │  │   OFFLINE       │
│ RAM  ████░░  8GB │  │ RAM  ███░░  6 GB │  │ Last seen 4m    │
│ NET  ██░░░  2.4M │  │ NET  █░░░  840K  │  │                 │
└──────────────────┘  └──────────────────┘  └─────────────────┘

Cluster Telemetry (aggregate)
100%│       ╭──────╮
 75%│ ╭─────╯      ╰──────╮         ╭──────────────────────
 50%│─╯                   ╰─────────╯
  0%│──────────────────────────────────────────────────────
     10:00   11:00   12:00   13:00   14:00   15:00
     ● CPU   ● RAM   ● Disk
```

Agent names show **aliases** (not real hostnames). Metrics filtered by `allowedMetrics`. Live via public WebSocket.

---

### 8.9 First-Time Setup & Login

**First-Time Setup** (`/setup`):

```
         ◈ CertainStats

  Welcome! Create your admin account.

  Username    ┌──────────────────────────────────┐
              │  admin                            │
              └──────────────────────────────────┘
  Password    ┌──────────────────────────────────┐
              │  ••••••••••••••••                 │
              └──────────────────────────────────┘
  Setup Token ┌──────────────────────────────────┐
  (from logs) │  a1b2c3d4e5f6...                 │
              └──────────────────────────────────┘

         [ Create Admin Account ]
```

**Login** (`/login`): same card, no setup token field, just username + password + "Sign In".

---

## 9. ASCII Wire-frames

> Legend: `█` = filled bar · `░` = empty track · `●` = online (animated glow) · `○` = offline

---

### Agent Hub — Grid View (Detailed Density)

```
╔═════════════════════════════════════════════════════════════════════════════════════╗
║  ◈ CertainStats     Agents   Dashboards   Alerts           ☾  ────  ⊙ Profile     ║
╠═════════════════════════════════════════════════════════════════════════════════════╣
║  ┌─ Sidebar ──────────┐  ┌─ Content ─────────────────────────────────────────┐    ║
║  │                    │  │                                                    │    ║
║  │ ● web-prod-01      │  │  Agent Hub  ╔ 6 Nodes ╗      ┌─────────────────┐ │    ║
║  │  CPU ████████░░    │  │             ╚═════════╝      │  ⊕  Add Agent   │ │    ║
║  │  RAM ██████░░░░    │  │                               └─────────────────┘ │    ║
║  │                    │  │  ┌─────────────────────────────────────────────┐  │    ║
║  │ ● db-primary       │  │  │ 🔍 Search by nickname, agent or OS... ⊞≡▤☰│  │    ║
║  │  CPU ███░░░░░░░    │  │  └─────────────────────────────────────────────┘  │    ║
║  │  RAM ██████░░░░    │  │                                                    │    ║
║  │                    │  │  ┌─ Bandwidth ──┐  ┌─ Disk I/O ──┐  ┌─ Traffic ─┐│    ║
║  │ ● cache-01         │  │  │  ↓ 2.4 MB/s  │  │ R  840 KB/s │  │ ↓ 1.23 TB ││    ║
║  │  CPU ████░░░░░░    │  │  │  ↑ 1.1 MB/s  │  │ W  210 KB/s │  │ ↑  890 GB ││    ║
║  │  RAM ███░░░░░░░    │  │  └─────────────-┘  └─────────────┘  └───────────┘│    ║
║  │                    │  │                                                    │    ║
║  │ ○ staging-01       │  │  ┌──────────────────────────┐  ┌──────────────────┐   ║
║  │  CPU ──────────    │  │  │ ●● web-prod-01         ⋮ │  │ ●● db-primary  ⋮ │   ║
║  │  RAM ──────────    │  │  │ Intel Xeon E5-2680        │  │ AMD EPYC 7402P   │   ║
║  │                    │  │  │ Linux 6.6.30-arch1        │  │ Linux 6.6.30     │   ║
║  │ ● worker-01        │  │  │                           │  │                  │   ║
║  │  CPU █████████░    │  │  │ CPU  ████████░░░  62.0%   │  │ CPU  ███░░░░  18%│   ║
║  │  RAM █████░░░░░    │  │  │      ██░░░░░░░░░   3.2%   │  │      ████░░  5.2%│   ║
║  │                    │  │  │ RAM  █████████░░   8.1 GB  │  │ RAM  ██████░  6GB│   ║
║  │ ● monitor-eu       │  │  │      ░░░░░░░░░░░   0 B     │  │      ██░░░░ 512M │   ║
║  │  CPU █████░░░░░    │  │  │ DSK  ████░░░░░░░  210 GB   │  │ DSK  ████████ 380│   ║
║  │  RAM ██████░░░░    │  │  │ NET  ██░░░░░░░░░   2.4 MB  │  │ NET  █░░░░░  840K│   ║
║  │                    │  │  │      ██████░░░░░   1.1 MB  │  │      ░░░░░░  120K│   ║
║  └────────────────────┘  │  │ IO   ████░░░░░░░  840 KB   │  │ IO   ██░░░░  210K│   ║
║                           │  │      ██░░░░░░░░░  210 KB   │  │      █░░░░░   80K│   ║
║                           │  │ ─────────────────────────  │  │ ──────────────── │   ║
║                           │  │ ⏱ 42d 7h 14m    BESZEL    │  │ ⏱ 12d 3h  LTSTATS│   ║
║                           │  └──────────────────────────┘  └──────────────────┘   ║
║                           │                                                        ║
║                           │  ┌──────────────────────────┐  ┌──────────────────┐   ║
║                           │  │ ○ staging-01           ⋮ │  │ ●● worker-01   ⋮ │   ║
║                           │  │ CPU  ░░░░░░░░░░░  ─ ─ ─  │  │ AMD Ryzen 9 5950X│   ║
║                           │  │ RAM  ░░░░░░░░░░░  ─ ─ ─  │  │ CPU  █████████ 74│   ║
║                           │  │                           │  │ RAM  █████░░░ 3.8G│   ║
║                           │  │  [ Node is OFFLINE ]      │  │ ⏱ 8d 11h  HETRIX │   ║
║                           │  │ Last seen 4m ago  BESZEL  │  └──────────────────┘   ║
║                           │  └──────────────────────────┘                         ║
║                           └────────────────────────────────────────────────────────┘
╚═════════════════════════════════════════════════════════════════════════════════════╝
```

---

### Agent Hub — List View

```
╔════════════════════════════════════════════════════════════════════════════════╗
║  ◈ CertainStats     Agents   Dashboards   Alerts                  ⊙ Profile  ║
╠════════════════╦═══════════════════════════════════════════════════════════════╣
║ ● web-prod-01  ║  Agent Hub  ╔ 6 Nodes ╗         ┌────────────────────┐      ║
║ ● db-primary   ║             ╚═════════╝          │  ⊕  Add Agent     │      ║
║ ○ staging-01   ║                                  └────────────────────┘      ║
║ ● worker-01    ║  ┌──────────────────────────────────────────────────────┐   ║
║ ● cache-01     ║  │ 🔍 Search...                          ⊞ ≡   ▣ ☰   │   ║
║ ● monitor-eu   ║  └──────────────────────────────────────────────────────┘   ║
║                ║                                                              ║
║                ║  ┌──────────────────────────────────────────────────────┐   ║
║                ║  │  AGENT          CURRENT LOAD             CPU   RAM  │   ║
║                ║  │ ────────────────────────────────────────────────────│   ║
║                ║  │ ● web-prod-01                                        │   ║
║                ║  │   Intel Xeon    CPU  ████████░░  62%  16c  16 GB   │   ║
║                ║  │   Linux 6.6.30  RAM  █████████░  8.1 GB             │   ║
║                ║  │                 DSK  ████░░░░░░  210 GB              │   ║
║                ║  │                 NET  ██░░░░░░░░  2.4 MB/s            │   ║
║                ║  │ ────────────────────────────────────────────────────│   ║
║                ║  │ ● db-primary                                         │   ║
║                ║  │   EPYC 7402P    CPU  ███░░░░░░░  18%  24c  32 GB   │   ║
║                ║  │   Linux 6.6.30  RAM  ██████░░░░  6.0 GB             │   ║
║                ║  │                 DSK  ████████░░  380 GB              │   ║
║                ║  │                 NET  █░░░░░░░░░  840 KB/s            │   ║
║                ║  │ ────────────────────────────────────────────────────│   ║
║                ║  │ ○ staging-01    OFFLINE              4c    8 GB     │   ║
║                ║  │   Last seen 4m ago                                   │   ║
║                ║  └──────────────────────────────────────────────────────┘   ║
╚════════════════╩═══════════════════════════════════════════════════════════════╝
```

---

### Agent Detail View

```
╔═══════════════════════════════════════════════════════════════════════════════╗
║  ◈ CertainStats     Agents   Dashboards   Alerts                 ⊙ Profile  ║
╠══════════════╦════════════════════════════════════════════════════════════════╣
║  ● web-prod  ║  ╔═══════════════════════════════════════════════════════════╗ ║
║  (active) ←  ║  ║  ╔ ● ONLINE ╗  ╔ BESZEL ╗  [✎ Rename][↺ Reinstall]     ║ ║
║              ║  ║  ╚══════════╝  ╚════════╝              [🗑 Terminate]    ║ ║
║  ● db-pri    ║  ║                                                           ║ ║
║  ○ staging   ║  ║  web-prod-01                                              ║ ║
║  ● worker    ║  ║  Production web server — EU West                          ║ ║
║  ● cache     ║  ║                                                           ║ ║
║  ● monitor   ║  ║  ┌───────────────┐ ┌───────────────┐ ┌────────────────┐  ║ ║
║              ║  ║  │ 🔵 CPU MODEL  │ │ 🟢 MEMORY     │ │ 🟡 STORAGE     │  ║ ║
║              ║  ║  │ Intel Xeon    │ │ 16.0 GB RAM   │ │ 2.0 TB total  │  ║ ║
║              ║  ║  │ E5-2680 v4    │ │  2.0 GB Swap  │ │ (3 mounts)    │  ║ ║
║              ║  ║  └───────────────┘ └───────────────┘ └────────────────┘  ║ ║
║              ║  ║  ┌───────────────┐ ┌───────────────┐ ┌────────────────┐  ║ ║
║              ║  ║  │ 🔵 CORES      │ │ 🔵 OS VERSION │ │ 🟢 UPTIME      │  ║ ║
║              ║  ║  │ 14C / 28T     │ │ Linux 6.6.30  │ │ 42d 7h 14m    │  ║ ║
║              ║  ║  └───────────────┘ └───────────────┘ └────────────────┘  ║ ║
║              ║  ║                                                           ║ ║
║              ║  ║  ┌─ ↓ Total RX ──────────┐  ┌─ ↑ Total TX ─────────┐   ║ ║
║              ║  ║  │  1.23 TB               │  │   890 GB              │   ║ ║
║              ║  ║  └───────────────────────-┘  └──────────────────────┘   ║ ║
║              ║  ╚═══════════════════════════════════════════════════════════╝ ║
║              ║                                                                ║
║              ║  [ 1h ] [ 6h ] [ 24h ◀ ] [ 7d ]             ▲ drag to zoom   ║
║              ║                                                                ║
║              ║  CPU USAGE                                                     ║
║              ║  100%│                                                         ║
║              ║   75%│          ╭─╮                                            ║
║              ║   50%│    ╭─────╯ ╰──╮  ╭──────╮                              ║
║              ║   25%│╭───╯           ╰──╯      ╰───────────────╮╭──╮         ║
║              ║    0%│─────────────────────────────────────────────────────   ║
║              ║      00:00  04:00  08:00  12:00  16:00  20:00  24:00          ║
║              ║      ● CPU Used  ● IO Wait  ● Steal                           ║
║              ║                                                                ║
║              ║  RAM USAGE                                                     ║
║              ║   16G│                                                         ║
║              ║   12G│────────────────────────────────────────────────────    ║
║              ║    8G│╭───────────────────────────────────────────────────╮   ║
║              ║    4G│╯                                                     ╰  ║
║              ║    0G│────────────────────────────────────────────────────    ║
║              ║      ● RAM Used  ● Swap                                        ║
║              ║                                                                ║
║              ║  NETWORK  RX / TX                 DISK I/O  Read / Write      ║
║              ║  5MB/s│  ╭╮                       5MB/s│  ╭╮ ╭╮               ║
║              ║  3MB/s│ ╭╯╰╮╭╯╰╮╭╯╰──╮            3MB/s│ ╭╯╰─╯╰╮  ╭╮         ║
║              ║  0MB/s│─╯   ╰╯  ╰╯   ╰────        0MB/s│─╯      ╰──╯╰────    ║
║              ║       ● RX  ● TX                         ● Read  ● Write      ║
╚══════════════╩════════════════════════════════════════════════════════════════╝
```

---

### Modals: Add Agent · Reinstall · Action Menu

```
┌─ Add Agent ──────────────────────┐       ┌─ Action Menu ─────────────────┐
│  ✕  Add a New Node               │       │  ●● web-prod-01            ⋮◄─┤
│ ─────────────────────────────── │       │  Intel Xeon · Linux 6.6.30   │
│                                  │       │                               │
│  ┌──────────────────────────┐    │       │  CPU  ████████░░░░  62%      │
│  │  ⌗  Beszel               │    │       │       ██░░░░░░░░░░   3%      │
│  │     Go / WebSocket       │    │       │  RAM  █████████░░░  8.1 GB   │
│  └──────────────────────────┘    │       └───────────────────────────────┘
│                                  │                              ▼ popover
│  ┌──────────────────────────┐    │       ┌────────────────────────────────┐
│  │  ▣  LTstats              │    │       │  ✎  Rename                     │
│  │     Lightweight C agent  │    │       │  ⊡  Reinstall                  │
│  └──────────────────────────┘    │       │ ──────────────────────────────│
│                                  │       │  🗑  Terminate          ← red  │
│          [  Cancel  ]            │       └────────────────────────────────┘
└──────────────────────────────────┘
```

```
┌─ Reinstall / Provision Modal ─────────────────────────────────────────────┐
│  Install Agent: web-prod-01                                    [✕ Close]  │
│ ─────────────────────────────────────────────────────────────────────────│
│  ① Agent token                                                             │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │  cs_tok_a1b2c3d4e5f6g7h8i9j0...                          [copy] │    │
│  └──────────────────────────────────────────────────────────────────┘    │
│  ② Install command                                                         │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │  curl -fsSL https://get.certainstats.io | sh              [copy] │    │
│  └──────────────────────────────────────────────────────────────────┘    │
│  ③ Server URL                                                              │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │  CERTAINSTATS_URL=https://panel.example.com               [copy] │    │
│  └──────────────────────────────────────────────────────────────────┘    │
│  ⚠  Keep this token secure. It authenticates this agent only.            │
└───────────────────────────────────────────────────────────────────────────┘
```

---

### UsageBar Tooltip · Alerts Panel

```
┌─ Sidebar compact bar (hover) ─────┐      ╔══════════════════════════════════════╗
│  ● web-prod-01                     │      ║  Alerting            [⊕ Create ▾]  ║
│  CPU  ████████░░  (hover here)     │      ╠════════════════════════════════════╣
│  RAM  ██████░░░░                   │      ║  Alert Rules & History | Preset Targets
└─────────────────────────────────── ┘      ║  ═══════════════════                ║
              ▼ portal tooltip              ║                                      ║
   ┌───────────────────────────┐           ║  ┌ ⚠ ACTIVE INCIDENTS (2) ────────┐ ║
   │  CPU                      │           ║  │ ⊗ staging-01  OFFLINE  [Manage]│ ║
   │ ─────────────────────────│           ║  │ ⊗ db-primary  CPU>85%  [Manage]│ ║
   │  ● Used        62.00 %   │           ║  └────────────────────────────────┘ ║
   │  ● IO Wait      3.20 %   │           ║                                      ║
   │  ● Steal        0.00 %   │           ║  (●) ☁ Node Offline  2m  [✎][🗑]    ║
   └───────────────────────────┘           ║      agent_down · Discord            ║
                                           ║  (●) ⧖ High CPU     5m  [✎][🗑]    ║
                                           ║      cpu_usage>85%                   ║
                                           ║  (○) 💾 Disk Space 10m  [✎][🗑]    ║
                                           ╚══════════════════════════════════════╝
```

---

### Public Dashboard · First-Time Setup

```
┌─ Public Dashboard (no auth) ───────────────────────────────────────────────┐
│                                                                              │
│  Production Cluster                       [🔗 Copy Shareable Link]          │
│  Last updated 12s ago                                                        │
│                                                                              │
│  ┌────────────────┐  ┌────────────────┐  ┌─────────────┐                   │
│  │ ● web-prod-01  │  │ ● db-primary   │  │ ○ staging   │                   │
│  │ CPU ████░  62% │  │ CPU ██░░  18%  │  │   OFFLINE   │                   │
│  │ RAM ████░   8G │  │ RAM ███░   6GB │  │ 4m ago      │                   │
│  │ NET ██░░  2.4M │  │ NET █░░░  840K │  │             │                   │
│  └────────────────┘  └────────────────┘  └─────────────┘                   │
│                                                                              │
│  100%│     ╭──────╮                                                         │
│   50%│╭────╯      ╰──────╮        ╭────────────────────────                │
│    0%│─╯                 ╰────────╯                                         │
│      10:00  11:00  12:00  13:00  14:00  15:00  ● CPU  ● RAM  ● Disk        │
└──────────────────────────────────────────────────────────────────────────────┘

┌─ First-Time Setup ─────────────────────────┐
│                                              │
│           ◈ CertainStats                   │
│                                              │
│  Welcome! Create your admin account.         │
│                                              │
│  Username    ┌──────────────────────────┐   │
│              │ admin                     │   │
│              └──────────────────────────┘   │
│  Password    ┌──────────────────────────┐   │
│              │ ••••••••••••••            │   │
│              └──────────────────────────┘   │
│  Setup Token ┌──────────────────────────┐   │
│              │ a1b2c3d4e5f6...           │   │
│              └──────────────────────────┘   │
│                                              │
│       [ Create Admin Account ]               │
└──────────────────────────────────────────────┘
```

---

## 10. Animation Catalogue

| Name | Trigger | Duration | Easing | Effect |
|---|---|---|---|---|
| `fadeIn` | `.animate-fade-in` on mount | 0.4s | ease-out | `opacity 0→1` + `translateY 10px→0` |
| `pulse-glow` | `.status-dot-online` always-on | 2s ∞ | linear | box-shadow `0→6px→0` green glow |
| `pulse-glow-micro` | `.status-pill.online` dot | 2s ∞ | linear | scale `0.9→1.2`, opacity `0.8→1` |
| `pulse-flash` | Agent card on WS live update | 0.4s | ease | background flash |
| `menuExpand` | Popover/action menu open | 0.12s | `cubic-bezier(0.16,1,0.3,1)` | scale `0.95→1` + `translateY(-4px→0)` |
| `modalScale` | Modal appear | 0.3s | `cubic-bezier(0.16,1,0.3,1)` | scale `0.95→1` + opacity |
| `spin` | Loading spinner (`.spinning`) | 1s ∞ | linear | `rotate(360deg)` |
| Hover lift | Agent card | 0.3s | `cubic-bezier(0.4,0,0.2,1)` | `translateY(-4px)` |
| Hover lift (spec tile) | `.spec-tile` | 0.2s | ease | `translateY(-2px)` |
| Btn press | `.btn-primary:active` | instant | — | `translateY(1px)` |
| Bar width | `UsageBar` fill change | 0.5s | ease | `width` transition |

---

## 11. Icon System

All icons are **Google Material Symbols Outlined** loaded via CDN in `index.html`:

```html
<link href="https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,100..700,0..1,-50..200" rel="stylesheet">
```

Usage: `<span className="material-symbols-outlined" style={{ fontSize: '20px' }}>hub</span>`

Key icons used throughout the app:

| Icon | Usage |
|---|---|
| `hub` | CertainStats logo |
| `add_circle` | Add Agent CTA |
| `more_vert` | Agent `⋮` action trigger |
| `search` | Search bar prefix |
| `grid_view` | Grid mode toggle |
| `view_list` | List mode toggle |
| `density_medium` | Detailed density |
| `density_small` | Simplified density |
| `schedule` | Uptime display |
| `dns` | Agent type label |
| `cloud_off` | Empty state (offline) |
| `terminal` | OS column, Reinstall |
| `edit` | Rename / edit |
| `delete_forever` | Terminate |
| `account_circle` | Profile button |
| `dark_mode` / `light_mode` | Theme toggle |
| `notifications_active` | Firing alert |
| `warning` | Active incident |
| `content_copy` | Copy to clipboard |
| `swap_vertical_circle` | Bandwidth stat card |
| `storage` | Disk I/O stat card |
| `cloud_sync` | Traffic stat card |

---

## 12. Scrollbar Styling

```css
::-webkit-scrollbar        { width: 6px; height: 6px; }
::-webkit-scrollbar-track  { background: transparent; }
::-webkit-scrollbar-thumb  { background: var(--border-hover); border-radius: 4px; }
::-webkit-scrollbar-thumb:hover { background: var(--text-muted); }
```

Thin (6px), no track, rounded thumb. Applied globally.

---

## 13. Frontend Directory Structure

```
frontend-admin/src/
├── App.tsx                      Route tree, setup check, auth guard
├── main.tsx                     Vite entry point
├── types/
│   └── index.ts                 All TypeScript interfaces (AgentSnapshot, Alert, etc.)
├── context/
│   └── AppContext.tsx           Global: isSidebarExpanded, showToast()
├── lib/
│   ├── api.ts                   fetchAPI wrapper (credentials: include, JSON)
│   ├── env.ts                   getPanelPath / getPublicPath
│   ├── utils.ts                 fmtBytes, fmtBps, fmtUptime
│   ├── UsageBar.tsx             ← Reusable bar component
│   └── TotalTelemetryPanel.tsx  ← Cluster stat cards
└── app/
    ├── globals.css              ← ALL design tokens + component styles
    ├── common/
    │   ├── PanelNav.tsx
    │   ├── AuthenticatedLayout.tsx   App shell + WebSocket connection
    │   ├── AddNodeModal.tsx
    │   ├── ReinstallModal.tsx
    │   ├── DeleteConfirmModal.tsx
    │   └── PayloadTemplateGuide.tsx
    ├── agent/
    │   ├── AdminPanel.tsx            Hub + detail route switch
    │   ├── AgentView.tsx             Grid/list view
    │   ├── AgentDetailView.tsx       Telemetry page
    │   ├── TelemetryChart.tsx        Recharts wrapper
    │   └── ScrollRestoration.tsx
    ├── alerts/
    │   ├── AlertsPanel.tsx
    │   ├── AlertCreateView.tsx
    │   └── AlertEditView.tsx
    ├── auth/
    │   ├── LoginView.tsx
    │   └── FirstTimeSetupView.tsx
    ├── dashboards/
    │   ├── DashboardCreateView.tsx
    │   └── DashboardEditView.tsx
    ├── management/
    │   └── ManagementView.tsx
    └── settings/
        ├── SettingsView.tsx
        ├── AlertTargetsTab.tsx
        ├── TargetCreateView.tsx
        └── TargetEditView.tsx
```

---

## 14. Conventions & Patterns

### CSS Classes vs Inline Styles

The codebase **intentionally mixes both**:
- **CSS classes** — for reusable design system atoms (`.card`, `.btn-primary`, `.badge`, `.glass-panel`)
- **Inline `style={{}}`** — for one-off layout, component-specific values, and dynamic values (metric widths, accent colors per card)

Do not reach for a utility class just to avoid an inline style. The goal is legibility, not purity.

### CSS Variables Everywhere

All design values (colors, shadows, transitions) must use CSS custom properties. **Never hardcode hex values for UI chrome.** Exception: metric colors are hardcoded in component arrays because they are data semantics, not theme tokens.

### No CSS Modules

Everything is in `globals.css`. Component-level overrides use inline styles. This keeps the project simple.

### Portal Usage

Two components render via `ReactDOM.createPortal(_, document.body)`:
- **`UsageBar` tooltip** — escapes `overflow: hidden` card containers
- **Action menu popover** — avoids z-index stacking context battles

Always use portals for floating UI that needs to escape the DOM hierarchy.

### State Management

No external state library. Three sources of truth:
1. **`useState`** — component-local UI state (modals open, filters, loading flags)
2. **`AppContext`** — `isSidebarExpanded` + `showToast()`
3. **WebSocket data** — `liveMetrics: Record<string, AgentSnapshot>` in `AuthenticatedLayout`, passed as props

### Scroll Restoration

`ScrollRestoration.tsx` saves/restores `main-panel` scroll position keyed by route. Prevents the hub from jumping to the top when navigating agent → hub → agent.

---

## 15. Known Design Gaps (Backlog)

- [ ] **Skeleton loading** — Currently a spinning `sync` icon. Premium card skeletons matching the agent card shape would be more polished.
- [ ] **Chart legend hover highlight** — Hovering a legend item should dim other series.
- [ ] **Sidebar agent search** — Unusable with many agents. Needs a filter input.
- [ ] **Toast notification elevation** — Slide-in animation + progress bar + typed variants (success/error/warning).
- [ ] **Theme cross-fade lag** — Body transitions in 0.3s but child components lag. Add `transition: background-color, border-color, color var(--transition-normal)` more broadly.
- [ ] **Agent card cascade reveal** — Stagger `animate-fade-in` delay per card index on first load.
- [ ] **Drag-to-reorder agents** in dashboard editor (currently text sort-key inputs only).
- [ ] **Chart height on mobile** — Agent detail charts need a reduced height at narrow widths.
- [ ] **Keyboard navigation** for action menus and dropdowns.

---

## Appendix: Backend Summary

> Full backend documentation is out of scope for this document. Quick reference only.

| Topic | Detail |
|---|---|
| Language | Go, single binary |
| HTTP router | `chi v5` |
| Time-series storage | Prometheus TSDB (`./data/tsdb`) |
| Relational storage | SQLite via `modernc.org/sqlite` |
| Real-time push | WebSocket (admin: `/api/ws`, public: `/api/public/ws/{id}`) |
| Agent submission | `POST /submit` (CBOR, token-authenticated) |
| Beszel compatibility | `GET /api/beszel/agent-connect` (WS + SSH key-pair) |
| Frontend embedding | `//go:embed` + `-tags embed` bakes both SPAs into the binary |
