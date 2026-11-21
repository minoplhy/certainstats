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
| `dnstap_qnames_minute` | Per-name counts over every query type and response code, cut by name, plus one "other" row per zone |
| `dnstap_qtypes_minute` | Name rows split by query type and response code, cut by row, plus "other" rows per zone, type and code |
| `dnstap_nets_minute` | Per-network counts, cut by network, plus one `(other)` network |
| `dnstap_netnames_minute` | Network × zone × name, cut by row, plus one row per zone with network and name `(other)` |
| `dnstap_netcodes_minute` | Network × zone × query type × response code, cut by row, plus one row per zone with network, type and code `(other)` |
| `dnstap_bounds_minute` | Per family (`qnames`, `qtypes`, `nets`, `netnames`, `netcodes`): `err`, how far any count can be off, and `unbounded`, minutes from collectors that don't report their cuts |
| `dnstap_names_minute` | No longer written. It held name rows cut by row; old rows stay until retention removes them, and are not read |
| `dnstap_clients_minute` | No longer written. It held client rows by network, zone, name, query type and response code for each minute's 50 busiest networks; old rows stay until retention removes them, and are not read |
| `dnstap_sizes_minute` | Response sizes |
| `dnstap_minutes` | Which minutes arrived, and whether they held only queries |
| `*_hourly` | The hourly rollups of the minute tables |

- Rows are replaced by key, so a resent part changes nothing.
- **Client network key.** `net` is 32 hex digits of the 16-byte network address (IPv4 as `::ffff:a.b.c.d`), `/`, and the 3-digit IPv6-form prefix length, e.g. `00000000000000000000ffff0a010200/120` for `10.1.2.0/24`. Text order is address order, so a CIDR filter is `net >= lo AND net < hi` on the `(user_id, net, bucket)` index. See `netkey.go`. The folded network is stored as `(other)`, which sorts before every key, so no CIDR range matches it.
- **Schema version.** `PRAGMA user_version` is 4. Version 3 added the `qnames`, `qtypes` and `bounds` tables, and version 4 adds `nets`, `netnames` and `netcodes`; both keep everything else. Opening a version 1 file (client rows by network alone) drops its `dnstap_*` tables and the `rolled_until` mark; zones are kept.
- **Hourly rollup.** Every hour, maintenance sums settled hours (older than two hours, after any resends) into the `_hourly` tables. Name tables (`qnames`, `qtypes`) and client lists (`nets`, `netnames`, `netcodes`) are cut with the bound rule at 1000 per collector and hour (see below), and the cut is added to the hour's `err`, which already sums the minutes' errors. The sums still match.
- **Which table is read.** Ranges that start within minute retention read minutes. Older ranges read hours, plus minutes for the hours not yet rolled. A breakdown by name reads `qnames`, or `qtypes` when a query type or response code filter needs the split rows. A name filter reads `qtypes`. Client answers read one client list (`clientTable` and `clientSource`):

  | Breakdown | Client list |
  | --- | --- |
  | networks, no other filter | `nets` |
  | networks, with a zone or name filter | `netnames` |
  | networks, with a type or code filter | `netcodes` |
  | names of a network | `netnames` |
  | types or codes of a network | `netcodes` |
  | zones of a network | `netnames` with a name filter, else `netcodes` |

  With a client filter, the total and series come from the list that applies the most filters (`nets` when there is no other filter), and what the rows leave out of it is added to `other` (`shortOther`). Rows below the cut keep only their zone: one folded row per network would grow with every network seen. A filter the list lacks (a type with names, a name with types) is reported in `ignored`. Sizes are not split by network. Both engines route the same way (`sourceOf`, `nameTable` and `foldTable` in `query.go`).

### Name and client counts and their error

Name lists and client lists are cut three times: by the collector each minute, by the hour rollup, and by the day rollup with the block engine. Every cut uses the same **bound rule** (`wire.Cut`):

- Rank the entries by count. If there are more than the cap, the cut is the count of the first entry past it; otherwise it is 0.
- Keep entries whose count is above the cut. Everything else, ties at the cut included, folds into "other", so no zone or name loses ties systematically.
- Record the cut. A collector sends its minute's cuts as `qname_err` (per-name counts) and `name_err` (split rows) with `bounded` set, and with client networks on, `net_err`, `net_name_err` and `net_code_err` for `nets`, `net_names` and `net_codes`. Networks are cut at `KNOT_DNSTAP_TOP` and client rows at `KNOT_DNSTAP_CLIENT_TOP`. Each coarser period adds its own cut to the errors it summed from its inputs.

A breakdown returns `err_bound`, the sum of every error in the range (for the collector, if one is filtered). Every listed count is at most that far below the truth, and a name not listed has a true count of at most `err_bound` plus the smallest listed count. Each cut is at most 1/1001 of its period's traffic, so `err_bound` stays under 0.3% of the range's traffic with three cuts. Kept counts are not reduced by the cut, so busy names show their true count.

- **Older collectors** send client rows (`clients`) instead of the three lists. Each list is summed from those rows and marked unbounded; a network whose rows span parts of a split minute keeps only one part's share, since rows replace by key.
- **Older collectors (names)** send no cuts and no per-name counts. Their per-name counts are summed from their rows, and their minutes count as `unbounded`, so the answer has `bounded: false` and the page shows the older "lower bounds" note. A busy minute split into several parts sums each part on its own, so such a name can be undercounted further.
- **Map overflow.** The collector keeps at most 50,000 keys per minute and map. Queries for new keys past that go straight to "other", and their number is added to the matching errors, so the bound still holds. A network past the cap is counted as `(other)` in every client list, so it adds to all three client errors.
- **Upgrading** starts name and client lists fresh: they read only the new tables, which begin at the upgrade. Totals and charts keep their history.

### `knot-topn/` (`KNOT_ENGINE=topn`)

dnstap rows go through one interface, `dnstapStore` (`dnstap.go`): `SaveDnstap`, `Dnstap`, `Rollup` and `Prune`. `Store` (knot.db) and `TopN` (`topnstore.go`) implement it. `Type.TopN` set means `TopN` holds the rows. `TopN` still writes `dnstap_minutes` in knot.db, so `LastDnstap` (freshness for the overview, checks and the quiet alert) reads the same table with either engine.

`TopN` is Knot's declaration on `internal/tblk`, certainstats' own block format for counts ("topn blocks"; see its `doc.go` and [custom storage formats](../storage-formats.md)). The engine and setting keep the name `topn`; the package is named after the format. The families are `totals`, `qnames` and `qtypes` (bounded: `Family.Bound` is `err`, with `unbounded` as a third measure), `nets`, `netnames` and `netcodes` (bounded the same way; see below), `clients` (no longer written), `sizes` and `minutes` (the queries-only flag). `names` is still declared so older blocks read, but it is no longer written or read. Each minute's cut errors are written as the bound row of each bounded family. Its block engine has three levels:

- **Minutes** are upserted into `head.db`. Every settled 15-minute slice, two hours after the fact as with the SQL rollup, is written as a block one family at a time and then removed from the head.
- **Hours.** Four slices merge into a 1-hour minute block, and the same pass writes the hour block with the cap of 1000 per collector applied (the bound rule for the name families). A quiet hour whose minutes fit one slice keeps that slice, and counts as rolled once its hour block exists.
- **Days.** Once a day is complete, its hour blocks merge into one and the day block is folded from them.

A merge writes its output before deleting its inputs. On open, a block that a merged block with a higher sequence number covers is deleted, so a crash between the two steps never counts rows twice. Retention and the size cap delete whole files.

Knot writes with `Append`, which replaces a minute's rows by key (a resent part changes nothing) and drops a minute older than the last sealed slice. `internal/tblk` also has features Knot doesn't use: `Add` (rows sum in, and late minutes still reach every tier), fold levels (`Key.Level`, for dependent keys such as ip, range and asn), value lists and exclusion in filters, and the `collector` filter and group. All Knot's folding keys share one level. The fold check flags `Folded` only when the key being read was folded. A parked study of CrowdSec on topn is in `artifacts/crowdsec-topn-2026-11-20/`.

Minute rows are kept as the collector sent them, and the hour cuts use the same rule as `Store`, so both engines give the same answers. The client families are `nets` (network, folding), `netnames` (network and name folding, zone kept) and `netcodes` (network, qtype and rcode folding, zone kept), all bounded. In `nets` every key folds, so the folded network row and the bound row are one row: its count is the folded traffic and its `err` the error. Only the part of a split minute that carries that row writes the error, so a bare bound row from another part never replaces it. `clients` is still declared so older blocks read, but it is no longer written or read.

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
| GET | `/collectors/knot/queries?dim=&hours=&collector=&zone=&qname=&qtype=&rcode=&client=&limit=` | dnstap breakdown by `qname`, `zone`, `qtype`, `rcode`, `client` or `size`, with the matching total and its series (every bucket of the window, quiet ones as 0). `client` is an address or CIDR; with it, everything but sizes is read from a client list (`exact: false`), and a prefix finer than /24 or /48 is widened and reported as `widened`. Name and client answers carry `err_bound` and `bounded`. |
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
  - the bound rule on rows, per-name counts and the three client lists: ties folding together, the cuts and map overflow sent as errors;
  - splitting busy minutes, with the bound fields in every part;
  - acknowledgements.
- `go test ./internal/collector/type/knot/...` covers:
  - the baseline, restarts and spreading over gaps, including exact sums;
  - per-zone and per-owner isolation;
  - zones, dnstap queries, rollup and prune, on both engines (`topnstore_test.go` for `topn`);
  - name and client errors adding up from minutes to hours, client routing and "not split by" on both engines, older collectors marked unbounded, and a version 2 knot.db keeping its history (`bound_test.go`);
  - RFC 1982 serials, drift and checks.
- `go test ./internal/tblk/` checks that the block engine gives the same answers as the package's SQL reference engine across tiers, retention and the size cap, and covers crash cleanup and byte-identical block writes. It takes a few minutes.
- `go test -tags knotreport -run TestTopNMatchesStore ./internal/collector/type/knot/` checks `TopN` against `Store` on generated traffic, error figures included. `TestFoldReport` (`storage_fold_report_test.go`) and `TestClientFoldReport` (`storage_client_fold_report_test.go`), both set by `FOLDREPORT_DIR`, measure name and client lists against an exact count; their results are in `artifacts/knot-fold-2026-11-20/`. The other `knotreport` tests are the storage reports in `artifacts/knot-storage-2026-11-19/`.
- `go test ./internal/collectoragent/...` runs the real collector against a fake Knot control socket and a real server, from `--check` to totals.
- `node --test web/tests/knot.test.cjs` covers the page.
