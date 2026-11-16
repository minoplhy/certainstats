# Knot DNS internals

This page covers how the Knot DNS collector type works inside: control socket handling, messages, storage, the API and tests. It is for developers. For setup and what the page shows, see [Knot DNS](../../operate/collectors/knot.md).

## Control socket

- **Command allowlist.** The collector sends only `status`, `stats`, `zone-stats` and `zone-status`, which knotd runs under its read lock. The check is in `internal/collectoragent/type/knot/ctl.go` and is covered by a test.
- **Counters only.** Only the `mod-stats` section of `stats` and `zone-stats` is kept. Other sections, such as `zone.size`, are current values rather than counters.
- **Partial failures.** When a command over every zone fails for some zones, Knot sends the good zones followed by one error. The collector keeps the good zones and logs the error.
- **Zone times.** `zone-status` is asked with the `u` flag for Unix times (Knot 3.3.3 and later). Older versions ignore it and print relative times, which are parsed too.
- **Connections.** Each read opens one connection, has a 5-second deadline, and closes again.

## Messages

Frames use the [collector protocol](README.md#protocol) with the `kn_` prefix. Wire types live in `internal/collector/type/knot/wire`.

| Direction | Message | Content |
| --- | --- | --- |
| server → collector | `kn_config` | Read intervals and caps |
| collector → server | `kn_status` | Whether Knot answers, its version, which options are on, the dnstap listener's state. Sent on change and every 5 minutes. |
| collector → server | `kn_stats` | One counter reading: section, item, id, zone and Knot's cumulative value |
| collector → server | `kn_zones` | Every zone's status, with absolute times |
| collector → server | `kn_dnstap` | One dnstap minute, or one part of a busy minute. Parts have disjoint rows. |
| server → collector | `kn_ack` | A stored dnstap part, or an error asking for a resend |

## Storage

### Counters

Stored in the collector TSDB as the metric `knot_counter`, with labels `user_id`, `collector_id`, `section`, `item`, `id` and `zone` (absent for server-wide counters).

- Values are stored as Knot reports them (cumulative), at the server's receive time.
- Increases are worked out at read time: baseline, restarts, and spreading over gaps, as described in [starting from zero](../../operate/collectors/knot.md#starting-from-zero). The spread gives the last bucket the remainder, so whole-number increases stay whole.

### `knot.db`

| Table | Holds |
| --- | --- |
| `zones` | Each collector's latest view of each zone, with the time its serial last changed |
| `dnstap_totals_minute` | Exact counts per zone, query type and response code |
| `dnstap_names_minute` | Name rows: the top names plus "other" rows, adding up to the totals |
| `dnstap_clients_minute`, `dnstap_sizes_minute` | Client networks and response sizes |
| `dnstap_minutes` | Which minutes arrived, and whether they held only queries |
| `*_hourly` | The hourly rollups of the minute tables |

- Rows are replaced by key, so a resent part changes nothing.
- **Hourly rollup.** Every hour, maintenance sums settled hours (older than two hours, after any resends) into the `_hourly` tables. Hourly names keep the top 1000 per collector and hour; the rest fold into "other" rows.
- **Which table is read.** Ranges that start within minute retention read minutes. Older ranges read hours, plus minutes for the hours not yet rolled.

## Alert evaluation

Rules are evaluated every minute in `internal/collector/type/knot/alerts.go`, through `collector.Round`. See [alert rules and incidents](README.md#alert-rules-and-incidents).

## Private API

Paths are relative to the panel's `/api`. Every GET answers one resource, and each filter takes one value.

| Method | Path | Behaviour |
| --- | --- | --- |
| GET | `/collectors/knot` | Servers with status, version, options and zone count; checks |
| GET | `/collectors/knot/zones` | Zones across servers, with each server's serial and a drift flag |
| GET | `/collectors/knot/zones/{zone}` | One zone on every server |
| GET | `/collectors/knot/totals?hours=&collector=&zone=` | Total requests, queries per second (over the time counted), responses, NXDOMAIN, SERVFAIL, REFUSED, transfers, bytes; `collecting` until the second reading |
| GET | `/collectors/knot/series?group=&hours=&collector=&zone=` | Per-second rates for `protocol`, `rcode`, `operation`, `qtype` or `bytes`; the top four lines plus "other" |
| GET | `/collectors/knot/queries?dim=&hours=&collector=&zone=&qname=&qtype=&rcode=&limit=` | dnstap breakdown by `qname`, `zone`, `qtype`, `rcode`, `client` or `size`, with the matching total and its series (every bucket of the window, quiet ones as 0) |
| GET | `/collectors/{id}/knot` | That collector's Knot state |

### Time range

The range is read as on every chart (`/api/metrics`, monitor history; `internal/metrics/timerange.go`):

- `hours`, or `start` and `end` in Unix ms in place of `hours` for a custom range.
- At most two years, ending no later than now. A bad range gets 400 `Invalid time range`.
- Responses carry the `start`, `end` and `step` they answered.
- The step is the shared chart step (about 1000 points per range) rounded up to whole minutes. It is rounded to whole hours instead when a dnstap range starts before minute retention and so reads hourly rows. The window is aligned to the step.
- An empty `collector` means every server of the owner.

## Tests

- `go test ./internal/collectoragent/type/knot/...` covers:
  - the control protocol against a fake Knot whose replies follow knotd's `knot/ctl/commands.c`: Unix and relative event times, `pending(…)`, `thawing`, non-counter sections, and a trailing error after good zones;
  - the command allowlist;
  - zone-status times;
  - the per-zone cap;
  - the dnstap Frame Streams handshake, decoding and aggregation, including the name cap and "other" rows adding up;
  - splitting busy minutes;
  - acknowledgements.
- `go test ./internal/collector/type/knot/...` covers:
  - the baseline, restarts and spreading over gaps, including exact sums;
  - per-zone and per-owner isolation;
  - zones, dnstap queries, rollup and prune;
  - RFC 1982 serials, drift and checks.
- `go test ./internal/collectoragent/...` runs the real collector against a fake Knot control socket and a real server, from `--check` to totals.
- `node --test web/tests/knot.test.cjs` covers the page.
