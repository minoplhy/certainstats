# Knot DNS

The Knot DNS [collector](README.md) type reads an authoritative Knot DNS server directly, without Prometheus exporters. This page covers what it reads, how to set it up, and what the page, checks and alerts show. It is for people running a server. Developers: see [Knot DNS internals](../../develop/collectors/knot.md).

On the host, the collector talks to Knot's **control socket**, the same one `knotc` uses. It can also receive Knot's **dnstap** stream, a per-query feed that Knot sends to a socket. The page is `/collectors/knot`, reached from the Collectors page; `/knot` redirects there.

The type is off unless the collector host sets `COLLECT_KNOT=true`, the same `COLLECT_<TYPE>` switch every type uses.

## What the collector reads

| Source | What | When |
| --- | --- | --- |
| Control socket, `stats` | Server-wide traffic counters from Knot's **mod-stats** module: requests by protocol, operations (query, update, notify, AXFR, IXFR), request and response bytes, response codes, query types | Always, every 60 seconds |
| Control socket, `zone-stats` | The same counters per zone | Only with `KNOT_ZONE_STATS=true`, every 60 seconds |
| Control socket, `zone-status` | Each zone's role, serial, next refresh, expiry, DNSSEC re-sign, freeze and errors | Always, every 5 minutes |
| Control socket, `status version` | Knot's version | On connect |
| dnstap | Each query and response, folded on the host into one-minute buckets | Only with `KNOT_DNSTAP=true` |

### Safety

Knot's control socket has no read-only mode: anything that can open it can stop Knot or purge zones. The collector therefore refuses to send any command except `status`, `stats`, `zone-stats` and `zone-status`, which Knot itself runs as read-only. Each read opens one connection with a 5-second deadline and closes it again, so the collector never holds Knot's socket.

### Starting from zero

Knot keeps its counters in memory only. They are totals since Knot last started, they reset when Knot restarts, and Knot keeps no history. CertainStats counts only what it has seen itself:

- **The first reading is a baseline** and counts as zero. A fresh install shows "Collecting, first numbers in about a minute" until the second reading, never Knot's lifetime total.
- **After that, each reading adds the difference** from the one before. A drop means Knot restarted, and the new value counts in full.
- **Increases are spread evenly** over the time between two readings. After a collector or panel outage, the gap fills the chart buckets it covers instead of spiking one, and no counts are lost while Knot keeps running.
- **A Knot restart** loses at most the queries between the last reading and the restart.
- **dnstap loses data while the collector is down**, because Knot drops dnstap messages it cannot send. Minutes already aggregated wait on the host (up to 60) until the server confirms them.

Zone status is current state rather than history, so zones, serials and drift show from the first reading.

## Setup

### 1. Turn on mod-stats in Knot

Without mod-stats, Knot reports almost no traffic counters, and the collector's `--check` warns.

```yaml
mod-stats:
  - id: default
    query-type: on      # for the Query types chart

template:
  - id: default
    global-module: mod-stats/default
    # Per-zone counters, only with KNOT_ZONE_STATS=true:
    # module: mod-stats/default
```

Then run `knotc reload`.

### 2. Optionally, turn on dnstap

The collector creates a Unix socket, by default `/run/certainstats/dnstap.sock`, and Knot connects to it:

```yaml
mod-dnstap:
  - id: tap
    sink: unix:/run/certainstats/dnstap.sock
    log-queries: off    # each response already stands for its query
    log-responses: on   # needed for response codes and sizes

template:
  - id: default
    global-module: [ mod-stats/default, mod-dnstap/tap ]
```

- Knot also accepts `tcp:<address>@<port>` sinks, but the collector listens on a Unix socket only.
- If Knot logs only queries, response codes are unknown, and the page says so.
- Dynamic updates (dnstap `UPDATE_*` messages) are not counted.
- The socket is owned by the `knot` user with mode 0660 when that user exists on the collector's host; otherwise its mode is 0666.

### 3. Add the collector

On the Collectors page, add a collector with Knot DNS ticked (`/collectors?add=knot`). Install it on the machine that runs Knot. See [collectors](README.md#setting-one-up).

### 4. Configure it

| Variable | Default | Meaning |
| --- | --- | --- |
| `COLLECT_KNOT` | off | `true` switches the type on |
| `KNOT_SOCKET` | `/run/knot/knot.sock` | Knot's control socket |
| `KNOT_ZONE_STATS` | off | `true` also reads per-zone counters |
| `KNOT_ZONE_STATS_MAX` | `200` | Most zones with per-zone counters, the busiest by requests (at most 1000) |
| `KNOT_DNSTAP` | off | `true` receives dnstap |
| `KNOT_DNSTAP_SOCKET` | `/run/certainstats/dnstap.sock` | Socket the collector creates for Knot's dnstap sink |
| `KNOT_DNSTAP_TOP` | `1000` | Name rows kept per minute (10 to 5000). The rest fold into one "other" row per zone, query type and response code. |
| `KNOT_DNSTAP_CLIENTS` | off | `true` also counts client networks (/24 and /48, top 50 per minute), split by zone, name, query type and response code, so you can filter by client network |
| `KNOT_DNSTAP_CLIENT_TOP` | `1000` | Client rows (network × name) kept per minute (10 to 5000). The rest fold into one "other" row per network, zone, query type and response code. |

### 5. Docker

Share the volume that holds Knot's control socket with the collector. The official Knot image keeps it in `/rundir`. For dnstap, also share the directory of the dnstap socket:

```yaml
services:
  knot:
    image: cznic/knot
    volumes:
      - knot-run:/rundir
      - dnstap:/run/certainstats
  certainstats-collector:
    image: ghcr.io/minoplhy/certainstats-collector:latest
    environment:
      COLLECT_KNOT: "true"
      KNOT_SOCKET: "/run/knot/knot.sock"
      KNOT_DNSTAP: "true"
    volumes:
      - knot-run:/run/knot
      - dnstap:/run/certainstats
volumes:
  knot-run:
  dnstap:
```

### 6. Check it

`certainstats-collector --check` reads Knot's version and counters, warns about missing mod-stats or per-zone counters, and tests the dnstap socket path without disturbing a collector already listening on it.

## Privacy

Raw queries never leave the host: dnstap is aggregated into minute buckets on the collector. Client addresses are counted only with `KNOT_DNSTAP_CLIENTS=true`, and then only as /24 or /48 networks.

With `KNOT_DNSTAP_CLIENTS=true`, each network's counts are split by the names it asked for, so anyone who can see the page can see which names a /24 or /48 looked up. Leave it off where that is too much.

## The page

- **Headline and totals.** A plain-language headline, then total requests, queries per second, NXDOMAIN and SERVFAIL shares, and zone transfers for the range.
- **Controls.**
  - A server picker.
  - The shared time picker, with quick ranges and a custom range up to two years. It is remembered in `certainstats_knot_hours`.
  - The shared **+ Filter** menu: zone, name, query type, response code, client network ("is" only). A client network is an address or a CIDR such as `10.0.0.0/8` or `2001:db8::/32`.
  - The range is kept in the URL as `hours` or `start`/`end`. Older links with `range=7d` open the same window.
  - Brushing (dragging across) any chart zooms the whole page to that range.
- **Checks**, described below.
- **Traffic.** One chart from the counters, with a switch between queries per second by protocol, response codes, query types, operations, and bytes per second. Only the chosen group is loaded. It is kept in the URL as `chart` when it is not protocol.
- **Queries** (with dnstap). The matching queries over time beside one ranked list, with tabs for names, query types, response codes, zones, response sizes and, when a server counts them, client networks.
  - The tab is kept in the URL as `top` when it is not names.
  - Clicking a row filters by it. Clicking a client network shows the names, types and codes it queried; the icon beside it copies it.
  - With a name filter, the client networks tab lists the networks that asked for that name.
- **Zones.** A dense list with roles, serial, soonest expiry and status. Clicking a zone opens it on every server.

### Which source answers a filter

Knot's mod-stats counters are separate groups: there is "AAAA queries" and there is "NXDOMAIN responses", but not "NXDOMAIN for AAAA". So:

| Filter | Source | Exact? |
| --- | --- | --- |
| Server | Counters | Yes |
| Zone | Counters with `KNOT_ZONE_STATS=true`, and dnstap | Yes |
| Name, query type, response code, or any mix | dnstap only | Totals by zone, query type and response code are exact. Anything filtered by name is a lower bound, since names below the per-minute cap are folded into "other". |
| Client network | dnstap with `KNOT_DNSTAP_CLIENTS=true` | A lower bound. Only each minute's 50 busiest networks are kept. Networks are /24 or /48, so a narrower filter (such as one address) is widened to its /24 or /48, and the page says so. |

The page says which source answers. When a filter only dnstap can apply, the counter charts keep showing all traffic and a note explains why.

## Checks

The page shows these with fixed limits. Alert rules use the same measures with limits you choose; see [alert rules](#alert-rules).

| Check | Level |
| --- | --- |
| Collector offline, or waiting for its first connection | bad, warn |
| Knot DNS not switched on at the collector (`COLLECT_KNOT=true`) | warn |
| Knot unreachable, with Knot's or the socket's error | bad |
| No mod-stats counters | warn |
| Per-zone cap leaves zones out (`KNOT_ZONE_STATS_MAX`) | warn |
| SERVFAIL above 1% of at least 100 responses over 15 minutes | warn |
| A secondary zone expires within 24 hours, or has expired | warn, bad |
| Serial drift: a server is behind the primary's serial for more than 15 minutes after the primary changed. Serials are compared with wrap-around (RFC 1982). | warn |
| A per-zone error reported by Knot | warn |
| dnstap switched on but the collector is not listening | bad |
| dnstap switched on but no traffic for 5 minutes | warn |

## Alert rules

How rules, targets and templates work in general is in [alerts](../alerts.md).

On the Alerts page, **New rule** offers a Knot DNS group under **When** once a collector reads Knot DNS.

- A rule watches a whole server or single zones. For zone conditions, "the whole server" means every zone of that server, including new ones.
- The threshold is the condition's single number; there is no operator or duration.
- Rules are evaluated every minute.

| Condition | Watches | Threshold (default) | Fires when |
| --- | --- | --- | --- |
| `knot_unreachable` | server | minutes (5) | Knot has not answered on its control socket, or the collector has not connected, for that long |
| `knot_servfail` | server | % (1) | SERVFAIL is above that share of at least 100 responses over 15 minutes |
| `knot_dnstap_quiet` | server | minutes (5) | dnstap is switched on but no traffic arrived for that long |
| `knot_expiry` | zone | hours (24) | A secondary zone expires within that time, or has expired |
| `knot_drift` | zone | minutes (15) | A server's serial is below the primary's, that long after the primary changed |

Incidents appear in the Alerts incident history like any other. Notification templates can use:

| Placeholder | Value |
| --- | --- |
| `{{NICKNAME}}` | The zone or server |
| `{{COLLECTOR}}` | The server's collector |
| `{{SUBJECT_KIND}}` | `server` or `zone` |
| `{{DETAIL}}` | What fired, such as "expires in 3.5 hours" |

- While a collector is offline, its zone incidents stay as they are; the unreachable rule is what judges it then.
- An incident closes without a notification when its rule is disabled, deleted or no longer covers the subject, or when the zone is gone from the server.

## Storage and retention

- **Counters** go to the collector TSDB (`collector_tsdb`), never the agent TSDB. They follow `COLLECTOR_RETENTION_DURATION`. See [collectors](README.md#storage).
- **Zones and dnstap** live in `DATA_DIR/knot.db`. Include it in [backups](../install-and-upgrade.md#back-up).
- **dnstap minutes** are kept for `KNOT_DNSTAP_RAW_RETENTION` (default 7 days).
- **dnstap hours** are kept for `KNOT_DNSTAP_ROLLUP_RETENTION` (default 90 days). Every hour, minutes older than two hours are summed into hours; hourly name lists keep the top 1000 names per collector and hour, and so do client rows.
- Retention covers every dnstap table, client rows included. Old rows are deleted on the hourly cleanup, so a row can outlive its cutoff by up to an hour. A retention of `0` keeps rows forever.
- **Upgrading to client rows split by name** resets the dnstap history in `knot.db` once (zones are kept), because the old client counts cannot be split. Upgrade the server and its collectors together: the server skips client rows from an older collector.
- Data and counters of deleted accounts are dropped.
