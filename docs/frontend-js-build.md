# Frontend assets: areas, sources and bundles

Static assets are split by who may download them. The larger browser scripts
are written as small ES modules under `web/src/js` and bundled with esbuild
into the single files the pages load. Each source file stays short enough to
read in one sitting.

The bundles are checked in. Normal Go builds, embedded builds and Docker builds
require no Node; Node is needed only to regenerate the bundles. The Go asset
pipeline minifies, fingerprints and adds integrity hashes to every static file
at startup.

## Static areas

`web/static` has one folder per audience. A router serves only the areas listed
for it in `internal/web/static.go`, and every other path is a 404:

| Area | Contents | Served by |
|---|---|---|
| `shared/` | What every page uses: `css/base.css`, fonts, `browser`, `disk_dom`, `requests`, `browser_protocol`, `telemetry`, `chart` | panel and public |
| `auth/` | Sign-in and first-time setup: `css/auth.css`, `js/setup.js` | panel, without a session |
| `admin/` | Panel pages: `css/admin.css`, `admin_core`, `admin_agents`, `network_monitors`, `admin_alerts`, `admin_modals`, `agent_management`, `dashboard_edit`, `incident_history`, `provision_renderer` | panel, signed-in only (`Cache-Control: private`) |
| `public/` | Status pages: `css/public.css`, `public_dashboard` | public |

So the public host can't fetch any admin or sign-in asset. On the panel, a
visitor who isn't signed in gets a 404 for every admin asset.

Each layout loads only its own areas:
- `layout/base.html` (signed-in panel) loads `shared` and `admin`;
- `layout/auth_base.html` (`login.html`, `setup.html`) loads `shared` and `auth`;
- `layout/public_base.html` loads `shared` and `public`.

Put a file in `shared/` only when both the panel and the public pages use it.
If only one side needs something, it goes in that side's area. `admin_core`
adds admin-only members (modals, the time picker, the network feed, sparklines)
onto the shared `CertainStatsTelemetry` and `CertainStatsChart` globals, so
`telemetry.js` and `chart.js` carry no admin code.

`base.css` holds the design tokens and every rule more than one side uses. The
area stylesheets load after it and hold the rules only that side uses. A few
admin rules stay in `base.css` on purpose: they compete with later shared rules
of equal specificity, and moving them would change which rule wins.

## Script sources

Every directory under `web/src/js/components/` or `web/src/js/pages/` that
contains an `index.js` is one bundle:
- `components/` holds building blocks;
- `pages/` holds one bundle per page script.

`AREAS` in `tools/js-bundle/build.cjs` maps each bundle to its area. The bundle
is written to `web/static/<area>/js/<name>.js`, and a bundle without an area
fails the build.

Inside a bundle, files are grouped into concern folders. The entry and
bundle-wide files (`index.js`, `state.js`, `init.js`, `mount.js`) sit at the
bundle root.

```
web/src/js/
  lib/                  stateless helpers any bundle may import (never a bundle)
    chart/              format, series, canvas helpers
    time_ranges.js      quick time ranges
    page_config.js      reads #page-config
  components/
    telemetry/          -> shared/js/telemetry.js        (CertainStatsTelemetry)
      ui/  feed/  page/  format/
    chart/              -> shared/js/chart.js            (CertainStatsChart)
      multi/            multi-series chart: draw, layers, tooltip, interaction
    admin_core/         -> admin/js/admin_core.js        (admin additions, CertainStatsModal)
      ui/  time/  feed/  chart/  page/  format/
    network_monitors/   -> admin/js/network_monitors.js  (CertainStatsNetworkMonitors)
      shared/  editor/  list/  panels/
  pages/
    admin_agents/       -> admin/js/admin_agents.js      (CertainStatsAdminAgents)
      nav/  hub/  detail/  editing/
    public_dashboard/   -> public/js/public_dashboard.js (CertainStatsPublicDashboard)
      nav/  detail/  time/
```

The import graph from `index.js` decides what goes into a bundle and in what
order. Folder and file names carry no ordering.

Small hand-written scripts such as `admin/js/admin_alerts.js` and
`shared/js/requests.js` stay as flat files in their area. Move a script into
`web/src/js` once it outgrows the size rule below.

## Rules

- **At most 400 lines per source file.** A test enforces this. Keep each file
  to one concern, such as notes editing, the detail view or live updates. If a
  file grows past the limit, split it by concern, and give a concern with
  several files its own folder. Don't move code into a "misc" file.
- **`index.js` is the entry.** It imports the parts, assembles the
  `window.CertainStatsX` namespace object, sets any back-compatibility
  `window.*` globals, and initialises the page from `#page-config`. Nothing else
  in the bundle writes to `window`.
- **Pages initialise themselves.** A template renders
  `<script type="application/json" id="page-config" data-module="…">` ahead of
  its page script. The script reads it when it loads: through `readPageConfig`
  from `lib/page_config.js` in a bundle, or directly from
  `document.getElementById('page-config')` in a hand-written script. There is no
  shared dispatcher, so no page ships another page's start-up code.
- **Shared mutable state lives in `state.js`** as one exported object, for
  example `export const state = { agentsData: [], activeRoute: null }`. Other
  modules read and write `state.agentsData`. ES modules cannot reassign another
  module's `let`, so a shared top-level `let` doesn't work. Read-only values
  are plain `export const`.
- **Bundles use each other through their window globals; never import across
  bundle folders.** For example, `admin_agents` calls
  `window.CertainStatsTelemetry.showToast()` and doesn't import from
  `../telemetry/`. Importing would copy that code into both bundles and create a
  second, separate copy of its state. The build fails if a module from
  `components/` or `pages/` ends up in two bundles. Only `lib/`, which holds
  stateless helpers, may be shared that way.
- **Per-instance components use a context object.** A component that can be
  mounted more than once on a page, such as network monitors, builds a `ctx`
  in `mount()`. That `ctx` holds the instance's elements, helpers and `state`.
  `mount()` passes it to factories such as `createEditor(ctx)` and
  `createList(ctx)`, whose returned functions are merged onto `ctx`. Parts call
  each other through `ctx` when they run, so the order the factories are called
  in doesn't matter.
- **No method-style `this`.** Namespace functions call each other directly
  instead of through `this.showToast()`. `this` inside a DOM event handler is
  fine.
- **Never edit a generated file.** Generated files start with
  `// Generated by tools/js-bundle/build.cjs ... Do not edit.` Change the
  sources under `web/src/js` and rebuild.

## Building

Install Node 22. The esbuild version is pinned in the tool's own package
manifest and lockfile, and matches the browser protocol tooling.

```sh
npm ci --prefix tools/js-bundle --no-audit --no-fund
node tools/js-bundle/build.cjs          # regenerate the bundles in web/static/<area>/js
node tools/js-bundle/build.cjs --check  # fail if any bundle is stale
npm ci --prefix web --no-audit --no-fund
node --test web/tests/*.test.cjs
go test ./...
```

Bundles are emitted unminified as an IIFE targeting ES2020, so diffs of the
generated files and browser stack traces stay readable. Minification happens in
the Go pipeline. Commit the sources and the regenerated bundle together.

## Adding a bundle

1. Create `web/src/js/pages/<name>/index.js` for a page script, or
   `web/src/js/components/<name>/index.js` for a building block, and its parts,
   grouped into concern folders.
2. Add the bundle to `AREAS` in `tools/js-bundle/build.cjs`, choosing the area
   of everyone who needs it, then run `node tools/js-bundle/build.cjs`.
3. Load it in the template with the usual tag, including the area:
   `<script src="{{.StaticPath}}/{{asset "admin/js/<name>.js"}}" integrity="{{integrity "admin/js/<name>.js"}}"></script>`.
   Put `#page-config` ahead of it if the page passes configuration.
4. Add the page to the route map in [design_frontend.md](design_frontend.md).

## CI

The `test-browser` job runs `node tools/js-bundle/build.cjs --check`, so a
source change without a rebuilt bundle fails. It also fails on a bundle edited
by hand.

`web/tests/browser.test.cjs` checks that:
- every source file is at most 400 lines;
- each generated file and source entry has its counterpart;
- each template references only the areas its visitors can fetch;
- no template loads `bootstrap.js`;
- the shared and public scripts and stylesheets contain no admin-only markers.

`internal/web/static_test.go` checks which areas each router serves, and that
admin assets need a session. The browser tests load the generated bundles from
`web/static/<area>/js`, which are the files the pages actually serve.
