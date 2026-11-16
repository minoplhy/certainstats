# Knot DNS

The Knot DNS [collector](collectors.md) type reads an authoritative Knot DNS
server directly, without Prometheus exporters. On the host it talks to Knot's
control socket, the one `knotc` uses, and can optionally receive Knot's dnstap
stream. The page is `/collectors/knot`, reached from the Collectors page;
`/knot` redirects there.

It is off unless the collector host sets `COLLECT_KNOT=true`, the same
`COLLECT_<TYPE>` switch as every type.

## What the collector reads

| Source | What | When |
| --- | --- | --- |
| Control socket, `stats` | Server-wide counters, including `mod-stats`: requests by protocol, operations (query, update, notify, AXFR, IXFR), request and response bytes, response codes, query types | Always, every 60 seconds |
| Control socket, `zone-stats` | The same counters per zone | Only with `KNOT_ZONE_STATS=true`, every 60 seconds |
| Control socket, `zone-status` | Each zone's role, serial, next refresh, expiry, DNSSEC re-sign, freeze and errors. Asked with the `u` flag for Unix times (Knot 3.3.3 and later); older versions ignore it and print relative times, which are read too | Always, every 5 minutes |
| Control socket, `status version` | Knot's version | On connect |
| dnstap | Each query and response, folded on the host into one-minute buckets | Only with `KNOT_DNSTAP=true` |

Knot's control socket has no read-only mode: anything that can open it can stop
Knot or purge zones. The collector therefore refuses to send any command but
`status`, `stats`, `zone-stats` and `zone-status`, which knotd itself runs under
its read lock; the check is in `internal/collectoragent/type/knot/ctl.go` and is
covered by a test. Only the `mod-stats` section is kept from `stats` and
`zone-stats`: other sections, such as `zone.size`, are current values rather
than counters. When a command over every zone fails for some zones, Knot sends
the good zones followed by one error; the collector keeps the good zones and logs
the error. Each read opens
one connection, has a 5-second deadline, and closes again, so the collector never
holds Knot's socket.

### Starting from zero

Knot keeps its counters in memory only. They are totals since Knot last started,
they reset when Knot restarts, and Knot keeps no history. CertainStats counts only
what it has seen itself:

- The first reading of each counter is a baseline and counts as zero. A fresh
  install shows "Collecting, first numbers in about a minute" until the second
  reading, never Knot's lifetime total.
- After that, each reading adds the difference from the one before. A drop means
  Knot restarted, and the new value counts in full.
- An increase is spread evenly over the time between its two readings. After a
  collector or panel outage, the gap fills the buckets it covers instead of
  spiking one, and no counts are lost while Knot keeps running.
- A Knot restart loses at most the queries between the last reading and the restart.
- dnstap is lossy by nature: while the collector is down, Knot drops dnstap
  messages. Minutes already aggregated wait on the host (up to 60) until the
  server acknowledges them.

Zone status is current state rather than history, so zones, serials and drift
show from the first reading.

## Setup

1. **Turn on mod-stats in Knot.** Without it Knot reports almost no traffic
   counters, and the collector's `--check` warns.

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

2. **Optionally, turn on dnstap.** The collector listens on a Unix socket it
   creates, by default `/run/certainstats/dnstap.sock`, and Knot connects to it:

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

   Knot also accepts `tcp:<address>@<port>` sinks; the collector listens on a
   Unix socket only. If Knot logs only queries, response codes are unknown and
   the page says so. Dynamic updates (dnstap `UPDATE_*` messages) are not counted.
   The socket is owned by the `knot` user with mode 0660 when that user exists
   on the collector's host; otherwise its mode is 0666.

3. **Add the collector** on the Collectors page with Knot DNS ticked
   (`/collectors?add=knot`). Install it on the machine that runs Knot.

4. **Configure it:**

   | Variable | Default | Meaning |
   | --- | --- | --- |
   | `COLLECT_KNOT` | off | `true` switches the type on |
   | `KNOT_SOCKET` | `/run/knot/knot.sock` | Knot's control socket |
   | `KNOT_ZONE_STATS` | off | `true` also reads per-zone counters |
   | `KNOT_ZONE_STATS_MAX` | `200` | Most zones with per-zone counters, the busiest by requests (at most 1000) |
   | `KNOT_DNSTAP` | off | `true` receives dnstap |
   | `KNOT_DNSTAP_SOCKET` | `/run/certainstats/dnstap.sock` | Socket the collector creates for Knot's dnstap sink |
   | `KNOT_DNSTAP_TOP` | `1000` | Name rows kept per minute (10 to 5000); the rest fold into one "other" row per zone, query type and response code |
   | `KNOT_DNSTAP_CLIENTS` | off | `true` also counts client networks (/24 and /48, top 50 per minute) |

5. **Docker.** Share the volume that holds Knot's control socket with the
   collector. The official Knot image keeps it in `/rundir`. For dnstap, also share
   the directory of the dnstap socket:

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

`certainstats-collector --check` reads Knot's version and counters, warns about
missing mod-stats or per-zone counters, and tests the dnstap socket path without
disturbing a collector already listening on it.

## Messages

| Direction | Message | Content |
| --- | --- | --- |
| server → collector | `kn_config` | Read intervals and caps |
| collector → server | `kn_status` | Whether Knot answers, its version, which options are on, the dnstap listener's state; sent on change and every 5 minutes |
| collector → server | `kn_stats` | One counter reading: section, item, id, zone and Knot's cumulative value |
| collector → server | `kn_zones` | Every zone's status, with absolute times |
| collector → server | `kn_dnstap` | One dnstap minute, or one part of a busy minute; parts have disjoint rows |
| server → collector | `kn_ack` | A stored dnstap part, or an error asking for a resend |

Wire types live in `internal/collector/type/knot/wire`.

## Storage

- **Counters** go to the collector TSDB (`DATA_DIR/collector_tsdb`, see
  [collectors](collectors.md#storage)), never the agent TSDB, as the metric
  `knot_counter` with labels `user_id`, `collector_id`, `section`, `item`, `id`
  and `zone` (absent for server-wide). Values are stored as Knot reports them, at
  the server's receive time; increases are worked out when read. Retention is
  `COLLECTOR_RETENTION_DURATION`.
- **Zones and dnstap** live in `DATA_DIR/knot.db`:
  - `zones` keeps each collector's latest view of each zone, with the time its serial last changed.
  - `dnstap_totals_minute` keeps exact counts per zone, query type and response code.
  - `dnstap_names_minute` keeps name rows: the top names plus "other" rows, adding up to the totals.
  - `dnstap_clients_minute` and `dnstap_sizes_minute` keep client networks and response sizes.
  - `dnstap_minutes` records which minutes arrived, and whether they held only queries.
  - Rows are replaced by key, so a resent part changes nothing.
- **Hourly rollup.** Every hour, maintenance sums settled hours (older than two
  hours, after any resends) into the `_hourly` tables. Hourly names keep the top
  1000 per collector and hour; the rest fold into "other" rows. Ranges that start
  within minute retention read minutes; older ranges read hours, plus minutes for
  the hours not yet rolled.
- **Retention.** Minutes are kept for `KNOT_DNSTAP_RAW_RETENTION` (default 7
  days) and hours for `KNOT_DNSTAP_ROLLUP_RETENTION` (default 90 days). Rows and
  counters of deleted accounts are dropped.

## Which source answers

Knot's mod-stats counters are separate groups: there is "AAAA queries" and
"NXDOMAIN responses", but not "NXDOMAIN for AAAA". So:

| Filter | Source | Exact? |
| --- | --- | --- |
| Server | Counters | Yes |
| Zone | Counters with `KNOT_ZONE_STATS=true`, and dnstap | Yes |
| Name, query type, response code, or any mix | dnstap only | Totals by zone, query type and response code are exact; anything filtered by name is a lower bound, since names below the per-minute cap are folded away |

The page says which source answers. When a filter only dnstap can apply, the
counter charts keep showing all traffic and a note explains why.

## Checks

Shown on the page with fixed limits. Alert rules use the same measures with
limits of their own; see [Alert rules](#alert-rules).

| Check | Level |
| --- | --- |
| Collector offline, or waiting for its first connection | bad, warn |
| Knot DNS not switched on at the collector (`COLLECT_KNOT=true`) | warn |
| Knot unreachable, with Knot's or the socket's error | bad |
| No mod-stats counters | warn |
| Per-zone cap leaves zones out (`KNOT_ZONE_STATS_MAX`) | warn |
| SERVFAIL above 1% of at least 100 responses over 15 minutes | warn |
| A secondary zone expires within 24 hours, or has expired | warn, bad |
| Serial drift: a server is behind the primary's serial (RFC 1982 arithmetic) for more than 15 minutes after the primary changed | warn |
| A per-zone error reported by Knot | warn |
| dnstap switched on but the collector is not listening | bad |
| dnstap switched on but no traffic for 5 minutes | warn |

## Alert rules

On the Alerts page, **New rule** offers a Knot DNS group under **When** once a
collector reads Knot DNS. A rule watches a server (all of it, which for zone
conditions means every zone of that server, new ones included) or single zones.
Its threshold is the condition's one number; there is no operator or duration.

| Condition | Watches | Threshold (default) | Fires when |
| --- | --- | --- | --- |
| `knot_unreachable` | server | minutes (5) | Knot has not answered on its control socket, or the collector has not connected, for that long |
| `knot_servfail` | server | % (1) | SERVFAIL is above that share of at least 100 responses over 15 minutes |
| `knot_dnstap_quiet` | server | minutes (5) | dnstap is switched on but no traffic arrived for that long |
| `knot_expiry` | zone | hours (24) | a secondary zone expires within that time, or has expired |
| `knot_drift` | zone | minutes (15) | a server's serial is below the primary's, that long after the primary changed |

Incidents appear in the Alerts incident history like any other, with
`{{NICKNAME}}` the zone or server, `{{COLLECTOR}}` the server's collector,
`{{SUBJECT_KIND}}` `server` or `zone`, and `{{DETAIL}}` what fired, such as
"expires in 3.5 hours". While a collector is offline its zone incidents are
kept as they are; an unreachable rule is what judges it then. An incident
closes without a notification when its rule is disabled, deleted or no longer
covers the subject, or the zone is gone from the server. Rules are evaluated
every minute (`internal/collector/type/knot/alerts.go`, through
`collector.Round`).

## Private API

Paths are relative to the panel's `/api`. Every GET answers one resource, and
each filter takes one value.

| Method | Path | Behaviour |
| --- | --- | --- |
| GET | `/collectors/knot` | Servers with status, version, options and zone count; checks |
| GET | `/collectors/knot/zones` | Zones across servers, with each server's serial and a drift flag |
| GET | `/collectors/knot/zones/{zone}` | One zone on every server |
| GET | `/collectors/knot/totals?hours=&collector=&zone=` | Total requests, queries per second (over the time counted), responses, NXDOMAIN, SERVFAIL, REFUSED, transfers, bytes; `collecting` until the second reading |
| GET | `/collectors/knot/series?group=&hours=&collector=&zone=` | Per-second rates for `protocol`, `rcode`, `operation`, `qtype` or `bytes`; the top four lines plus "other" |
| GET | `/collectors/knot/queries?dim=&hours=&collector=&zone=&qname=&qtype=&rcode=&limit=` | dnstap breakdown by `qname`, `zone`, `qtype`, `rcode`, `client` or `size`, with the matching total and its series (every bucket of the window, quiet ones as 0) |
| GET | `/collectors/{id}/knot` | That collector's Knot state |

The time range is read as on every chart (`/api/metrics`, monitor history):
`hours`, or `start` and `end` in Unix ms in place of `hours` for a custom range,
at most two years and ending no later than now. A bad range gets 400 `Invalid
time range`. Responses carry the `start`, `end` and `step` they answered. The
step is the shared chart step (about 1000 points per range) rounded up to whole
minutes, or to whole hours when a dnstap range starts before minute retention
and so reads hourly rows; the window is aligned to it. An empty `collector`
means every server of the owner.

## The page

- A plain-language headline, then total requests, queries per second, NXDOMAIN
  and SERVFAIL shares, and zone transfers for the range.
- A server picker, the shared time picker (quick ranges and a custom range,
  remembered in `certainstats_knot_hours`) and the shared **+ Filter** menu
  (zone, name, query type, response code; "is" only), kept in the URL as `hours` or `start`/`end`. Older links with
  `range=7d` open the same window. Brushing any chart zooms the whole page to
  that range.
- Checks, then **Traffic**: one chart from counters with a switch between
  queries per second by protocol, response codes, query types, operations, and
  bytes per second. Only the chosen group is read; it is kept in the URL as
  `chart` when it is not protocol.
- With dnstap, **Queries**: the matching queries over time beside one ranked
  list, with tabs for names, query types, response codes, zones, response sizes
  and, when a server counts them, client networks. One request answers both the
  chart and the list; the tab is kept in the URL as `top` when it is not names.
  Clicking a row filters by it; a client network, which has no filter, is
  copied instead.
- **Zones**: a dense list with roles, serial, soonest expiry and status. Clicking a
  zone opens it on every server.

## Privacy

Raw queries never leave the host: dnstap is aggregated into minute buckets on the
collector. Client addresses are counted only with `KNOT_DNSTAP_CLIENTS=true`,
and then only as /24 or /48 networks.

## Verification

- `go test ./internal/collectoragent/type/knot/...` covers:
  - the control protocol against a fake Knot whose replies follow knotd's
    `knot/ctl/commands.c`: Unix and relative event times, `pending(…)`,
    `thawing`, non-counter sections, and a trailing error after good zones
  - the command allowlist
  - zone-status times
  - the per-zone cap
  - the dnstap Frame Streams handshake, decoding and aggregation, including the name cap and "other" rows adding up
  - splitting busy minutes
  - acknowledgements
- `go test ./internal/collector/type/knot/...` covers:
  - the baseline, restarts and spreading over gaps
  - per-zone and per-owner isolation
  - zones, dnstap queries, rollup and prune
  - RFC 1982 serials, drift and checks
- `go test ./internal/collectoragent/...` runs the real collector against a fake
  Knot control socket and a real server, from `--check` to totals.
- `node --test web/tests/knot.test.cjs` covers the page.
