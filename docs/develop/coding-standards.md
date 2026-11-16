# Coding standards

This page sets the coding rules for the Go backend and the browser frontend. It is for anyone changing the code, people and AI agents alike. Related pages: [architecture](architecture.md), [frontend](frontend.md), [frontend JavaScript build](frontend-js-build.md), [caching standards](caching-standards.md), [testing](testing.md).

## Principles

1. **Low overhead first.**
   - Few or no allocations in hot paths: TSDB writes, agent ingestion, WebSocket broadcast loops.
   - Fast query responses and small in-memory caches.
2. **Few dependencies.**
   - Backend: the Go standard library wherever practical; no heavy middleware frameworks.
   - Frontend: modular vanilla JavaScript and CSS design tokens. No client-side framework (React, Vue, Angular) in production pages.
3. **Correct state.**
   - Lifetime counters are never mixed up with, or overwritten by, short-interval rates.
   - Concurrency is ordered so that deadlocks and data races cannot happen.

## Go

### Structure

- `cmd/` holds executable entrypoints only, such as `cmd/certainstats`. Business logic lives in `internal/`.
- `internal/` packages are grouped by responsibility. The package list is in [architecture](architecture.md#code-layout).

### Building

Don't write binaries into the repository. When a tool, script or CI step only needs to check that the build works, send the output to `/dev/null`:

```bash
go build -o /dev/null ./cmd/certainstats
```

### Errors and logging

- Handle errors at the call site, with context:
  ```go
  if err != nil {
      return fmt.Errorf("agent %s: failed to increment traffic: %w", agentID, err)
  }
  ```
- Don't swallow errors silently. If an error is deliberately ignored, say why in a comment.
- Log operational events, warnings and errors through `internal/logger`.
- Diagnostic output (memory logging, profiling, debug detail) is off by default, each behind its own environment variable.

### Concurrency

- Use `sync.RWMutex` where reads far outnumber writes.
- Keep critical sections small. No slow I/O or network calls while holding a lock.
- Follow a fixed lock order across modules, so deadlocks cannot happen.
- Pass `context.Context` through every blocking operation and long-running routine, so they can be cancelled cleanly.

### Databases (SQLite and TSDB)

- **Parameterised queries only.** Use `?` placeholders; never build SQL by string concatenation or formatting.
- **Transactions** defer a rollback and end with an explicit commit:
  ```go
  tx, err := s.db.BeginTx(ctx, nil)
  if err != nil {
      return err
  }
  defer tx.Rollback()

  // statements...

  return tx.Commit()
  ```
- **Schema changes** are declared idempotently in `internal/store/sqlite/migrate.go` (and its `*_migrate.go` siblings).
- **Prefer existing columns to new schema.** Before adding a column, table or index, check whether an existing column is unused at that point in the row's life.

## Frontend

### No scripts in templates

Keeping code out of templates keeps the Content Security Policy and Subresource Integrity working.

1. **Partials are markup only.** Never put `<script>` or `<style>` in a partial template (`web/templates/partials/…`).
2. **Pages pass data as inert JSON.** A top-level page template passes server data only as JSON in `#page-config`, right before the page's own script. The script reads it and starts itself; there is no inline code and no shared dispatcher:
   ```html
   <script type="application/json" id="page-config" data-module="CertainStatsAdminAgents">{{bootstrap .}}</script>
   <script src="{{.StaticPath}}/{{asset "admin/js/admin_agents.js"}}" integrity="{{integrity "admin/js/admin_agents.js"}}"></script>
   ```
3. **No state in HTML.** Variables and modal state belong in the page's JavaScript module, never in a template.
4. **No inline event handlers.** Attach listeners with `addEventListener` in the script files.

### JavaScript modules

Asset areas, bundle sources, the 400-line limit and the build are described in [frontend JavaScript build](frontend-js-build.md). In short: edit sources under `web/src/js/`, never the generated bundles.

### In-page navigation

- The agent hub switches between the overview and an agent's detail view in place, with the History API (`pushState`, `popstate`) via `initRouter`.
- Routes: `/` is the overview; `/{agent_id}` shows that agent's detail view in place.
- Don't add standalone duplicate pages for views the in-page router already shows.

### Editing text in place

- Short metadata, such as agent nicknames and notes, is edited in place: clicking the text turns it into an input or textarea with Save and Cancel. Don't open a dialog for simple text edits.
- Notes:
  - **Header widget:** shows `| [No notes / short text / See below] notes` in the agent header. Clicking opens an input in the header bar.
  - **Expanded section:** appears below the specifications when a note is long (over 40 characters, or several lines), with an in-place textarea.

### No emojis

Dashboards, install dialogs, copyable command blocks and alert forms use no emojis (such as 📋 ✓ 📄 ⚠️ ⏳). Use inline SVG icons and plain text. Dialog behaviour (native `<dialog>`, closing on backdrop click) is described in [frontend](frontend.md).

### Live updates change the DOM in place

- Live updates arrive every ten seconds. Never rebuild whole containers with `innerHTML = …` on each update.
- Change the specific elements with `textContent`, `className` and `style.width`:
  ```javascript
  const elUsed = document.getElementById('disk-used-' + safePath);
  if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(d.used_bytes);
  ```
- When switching views, render the remembered state at once, without a blank loading flash.

### Lifetime counters versus live rates

- **Lifetime counters** (total network traffic, total disk read and write) are loaded from the database and kept in state (`agentsData`).
- **Live updates** (`snap.disks`, `snap.rx_bps`, …) update capacity percentages, throughput gauges and chart points. They must never overwrite lifetime counters with interval values.
- Private and public pages use separate localStorage keys, e.g. `certainstats_active_hours` vs `certainstats_public_active_hours`.

### Styling

- Use the CSS custom properties defined in `web/static/shared/css/base.css`. The token names and roles are listed in [frontend](frontend.md#design-tokens). Prefer the current names (`--bg`, `--surface`, `--text`, `--line`, `--accent`, …) over the legacy aliases (`--bg-primary`, `--accent-primary`, …).
- Rules used by only one kind of page go in that area's stylesheet: `admin/css/admin.css`, `public/css/public.css` or `auth/css/auth.css`.
- Never hard-code colours, so the light and dark themes keep working.
- Fonts come from tokens too: `--font-mono` for every number, rate, timestamp, ID, command and hash; the heading and body tokens for everything else.

## Caching

Follow [caching standards](caching-standards.md). In short:
1. All cache lookups, stores and invalidations go through `internal/context`.
2. Cache entries hold pre-compressed zstd and gzip payloads.
3. No SHA-256 ETags on telemetry endpoints.
4. Invalidate explicitly on every change (dashboard edits, agent revocations).

## Don't

| Don't | Why | Instead |
|---|---|---|
| `<script>` inside partial templates | Runs twice, leaks state, breaks CSP and SRI | Put the code in a script under `web/static/<area>/js/` or its bundle sources |
| Emojis in technical or install UI | Clutters the interface | Inline SVG and plain text |
| A dialog for simple note editing | Extra clicks | In-place editing |
| Standalone duplicate page routes | Duplicate templates, breaks in-page navigation | The in-page router (`/{agent_id}`) |
| `agent.disks = snap.disks;` | Overwrites lifetime totals with a ten-second interval value | Update `d.used_bytes` in place; keep `d.read_bytes` |
| Editing a generated `web/static/<area>/js/<bundle>.js` | The next build overwrites it and CI rejects the mismatch | Edit `web/src/js/` and run `node tools/js-bundle/build.cjs` |
| `go build -o certainstats ./cmd/certainstats` | Leaves a binary in the repository root | `go build -o /dev/null ./cmd/certainstats` |
| Rebuilding DOM with `innerHTML` in live loops | Destroys nodes, causes layout thrashing, resets scroll | Update specific elements with `textContent` or `style` |
| Hard-coded colours (e.g. `#121318`) | Breaks theme switching | CSS custom properties (`var(--surface)`) |
| Frontend frameworks or CDN scripts | Supply-chain risk, size, latency | Vanilla JS and Go templates |
| SQL built by concatenation | SQL injection | Parameterised queries (`?`) |

## Before you submit

```bash
go test ./...
go build -o /dev/null ./cmd/certainstats
```

Every store and helper package keeps working unit tests, and the code compiles without errors or warnings. The full list of checks is in [testing](testing.md).
