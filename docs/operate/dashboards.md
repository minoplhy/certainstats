# Status pages

A **status page** (called a dashboard in the code and URLs) is a public page that shows a chosen set of your agents to anyone with the link, without signing in. This page covers creating one, choosing what visitors can see, and what the page looks like. It is for people running a server.

Status pages are served on the public side: at `PUBLIC_URL`, or under `/dashboard` by default. See [configuration](configuration.md#where-the-panel-and-dashboards-live).

## Creating a status page

On the **Dashboards** page, create a new status page. The editor has four parts:

### General

| Field | Meaning |
| --- | --- |
| **Title** | Shown at the top of the page, e.g. "US East cluster". Required. |
| **Address** | The page's slug: lowercase letters, numbers and dashes. The page lives at `<public URL>/<slug>`. Left empty on create, it is made from the title. Each address must be unique. |
| **History visitors can see** | How far back visitors' charts can go: 1, 2, 7, 30, 90 or 180 days, 1 year or 2 years. Default 7 days. |

### Server details

Hardware and status facts visitors can see, each switched on or off: online status, uptime, Linux version, CPU model, CPU cores, RAM size, swap size, disk size.

### Metrics

Live values and history charts visitors can see: CPU usage, CPU I/O wait, CPU steal, RAM used, swap used, disk used, disk read, disk write, download, upload.

A new status page starts with every detail and metric switched on. **If you switch everything off, the page shows nothing publicly**: it returns "Public access disabled" rather than an empty page.

### Servers

- Tick the agents to show. Search filters the list, and **Select all** ticks every one.
- **Shown as:** each ticked agent gets a public name, defaulting to its own name. Visitors never see the agent's real ID or name unless you leave it.
- **Order:** alphabetical unless you change it. Drag rows, or use the arrow buttons, to set your own order; **Reset to alphabetical** undoes it.

Save with **Create status page** or **Save changes**. Deleting a status page asks for confirmation, and its public link stops working at once.

## What visitors see

- **Status banner:** "All systems operational", or a partial outage when some servers are offline (only when online status is shown).
- **Fleet figures** and a list of servers, as status rows (the default) or cards. The visitor's choice is remembered in their browser.
- **Server detail** at `<public URL>/<slug>/<public ID>`: the same layout as the admin detail view, without admin actions, limited to what you allowed.
- **Charts** with a range bar limited to the history you allowed.
- Values update live every ten seconds.

Visitors only ever see what the page allows. Hidden values are removed on the server before anything is sent, in the HTML, the JSON API and the live feed alike.

## Privacy and safety

- Each agent on a status page has a separate public ID, so its real agent ID is never exposed.
- Changing a status page's settings or servers takes effect at once: cached copies are dropped and open visitor connections are closed and re-checked.
- Status pages are cached for up to 60 seconds and rate-limited per visitor IP. See [security](security.md#public-dashboards) and [security: limits](security.md#limits).
- Network monitors, collectors and alerts never appear on status pages.
