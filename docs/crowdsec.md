# CrowdSec

CrowdSec is a [collector](collectors.md) type. A collector beside CrowdSec reads
its alerts from the Local API and sends them to CertainStats, where the CrowdSec
page explores them. Alerts are private and owner scoped; nothing reaches public
dashboards. No change to CrowdSec's profiles or notification plugins is needed.

## What the collector reads

The collector logs in to the Local API as a machine (`POST /v1/watchers/login`)
and lists alerts (`GET /v1/alerts`) with every decision attached. It only reads,
but a machine login could also write alerts and delete decisions, since CrowdSec
has no read-only machine. The setup steps therefore register a machine used only
by the collector, which can be removed on its own.

It drops each alert's `events` and `meta`, which carry raw log lines, before
anything leaves the host.

It asks for `include_capi=false`, as `cscli alerts list` does, so community
blocklist and subscribed list pulls are left out. Each of those arrives as one
alert holding thousands of decisions, and none is an attack seen on this host.
An alert with more than 500 decisions is sent with the first 500. Batches are
split by encoded size as well as count, so each fits one 1 MiB frame.

Alerts are found by their start time, with `since` and `until` sent as durations
before now. If a Local API rejects that form, the collector switches to RFC 3339
timestamps. Each pass starts an hour before the cursor, because a long bucket
can be created well after it started; the server ignores alerts it already has,
and the collector skips alerts it already sent on the same connection. A window
that fills a page of 1,000 alerts is split until it doesn't, so nothing is
skipped. The Local API reports a decision's time left when it is read; the
collector turns that into an expiry time.

These Local API details were taken from CrowdSec's API specification and should
be confirmed against a real host: how `since` is matched, and the default page
size.

## Setup

On top of the [steps every collector needs](collectors.md#setting-one-up):

1. Tick CrowdSec when creating the collector, and set `COLLECT_CROWDSEC=true` on the host.
2. Install the collector on the machine that runs the Local API. In a
   multi-server setup that is the central server only. One collector per Local API.
3. Register its machine login, as root:

   ```sh
   mkdir -p /etc/certainstats
   cscli machines add certainstats --auto -f /etc/certainstats/crowdsec-lapi.yaml
   chmod 600 /etc/certainstats/crowdsec-lapi.yaml
   ```

   The file holds `url`, `login` and `password`. `CROWDSEC_LAPI_CREDENTIALS`
   points to it and defaults to that path. `--check` warns when it is readable by
   other users.
4. If CrowdSec runs in Docker, the collector must share its network, the Local
   API must listen on that network and not only on 127.0.0.1, `url` in the file
   must name the container (`http://crowdsec:8080`), and the file must be
   mounted into the collector.

Not needed: an open port, a bouncer key, or access to CrowdSec's log files.

When the Local API is unreachable the collector stays connected and reports the
error, which the Collectors dialog and the page headline show.

## Messages

| Direction | Message | Content |
| --- | --- | --- |
| server → collector | `cs_config` | Cursor, poll interval (60 s), batch size (200), overlap (3,600 s); sent after `hello` |
| collector → server | `cs_status` | Local API reachable or its error, machine login, Local API URL; sent when it changes |
| collector → server | `cs_alerts` | Batch ID and up to 200 alerts |
| server → collector | `cs_ack` | Batch ID, stored, duplicates, cursor; or an error and when to resend |
| server → collector | `cs_resync` | Read again from a given time |

The collector sends the next batch only after the previous `cs_ack`. A batch
over 200 alerts closes the connection. If storing fails the server answers with
an error and the cursor stays put. The cursor is the newest alert start time
stored for the collector, and only moves forward.

## Storage

Alerts live in their own SQLite file, `DATA_DIR/crowdsec.db`, so long queries
never hold up agent ingestion or the main state database. It has three tables:

- `alerts`: one row per alert, unique on owner and CrowdSec's alert `uuid`, so a
  repeated delivery stores nothing. An alert without a uuid gets a stable one
  from its collector, Local API ID and creation time. The alert's time is its
  `start_at`, never when it arrived. `action` lists the distinct decision types
  (`ban`, `ban+captcha`) or `none`.
- `decisions`: every decision of every alert, with its expiry.
- `rollup_hourly`: alert and event counts per owner, hour, scenario, country,
  machine and action, written in the same transaction as each new alert.

### Retention

| Variable | Default | Meaning |
| --- | --- | --- |
| `CROWDSEC_RAW_RETENTION` | `0` (unlimited) | How long alerts and their decisions are kept, in Go duration syntax such as `4320h` |
| `CROWDSEC_ROLLUP_RETENTION` | `0` (unlimited) | How long hourly counts are kept |

Values below one hour are rejected at startup. Pruning runs with the hourly
maintenance in batches of 5,000 rows, removes rows of deleted accounts, and logs
the table sizes. With raw alerts kept for 180 days and counts unlimited, charts
reach back years while disk use stays bounded.

## Which table answers

Every read needs a time range. A filter on IP, range, ASN or collector, or a
search, reads raw alerts. Otherwise ranges longer than seven days, or ranges
older than the retained raw alerts, read the hourly counts. When detail is asked
for a period whose raw alerts were pruned, responses carry
`detail_expired_before` and the page says so rather than showing an empty result.
Unique IPs are not counted from hourly data.

## Private API

Paths are relative to the panel's `/api`.

| Method | Path | Behaviour |
| --- | --- | --- |
| GET | `/collectors/{id}/crowdsec` | The collector's CrowdSec status, cursor and machine |
| POST | `/collectors/{id}/crowdsec/resync` | Ask the connected collector to read again from `since_ms` |
| GET | `/collectors/crowdsec/summary` | Alerts, events, unique IPs, active decisions by type, and coverage |
| GET | `/collectors/crowdsec/timeline` | Alert counts per bucket, at most 1,000 buckets, zero-filled |
| GET | `/collectors/crowdsec/groups/{field}` | Top values for one field; `limit` 1 to 100 |
| GET | `/collectors/crowdsec/alerts` | Paged alerts: `page`, `limit` (25, 50, 100), `sort`, `order` |
| GET | `/collectors/crowdsec/alerts/{id}` | One alert with all decisions |
| GET | `/collectors/crowdsec/ips/{ip}` | First and last seen, totals, scenarios, machines and latest 50 decisions over retained alerts |

Data reads take `start` and `end` (Unix ms, at most 20 years apart) or `hours`,
and filters on `ip`, `range`, `scenario`, `country`, `machine`, `asn`, `action`
and `collector`: `field=value` includes, `not_field=value` excludes, and a
parameter is repeated for several values. Values are never split on commas.
`q` searches IP, scenario, machine, country, network and range, with wildcard
characters taken literally. `tz_offset` (minutes east of UTC) aligns day
buckets; the browser labels every timestamp itself. A failed query returns an
error, never zeros.

## The page

`/crowdsec` opens with a sentence headline and figures: alerts, unique IPs,
active bans now and collectors reporting. Below are the filter bar (search,
"Add filter", removable chips, the shared time picker and an **All** range
starting at the oldest data), the alert timeline (brush to zoom), four
breakdowns whose field can each be changed, and the alert table. Clicking a value
in a breakdown filters in place; its second button excludes it. Table cells have
copy and filter buttons, columns sort, and a row expands to show every decision.
Clicking an IP opens its own view. The range, filters, sort, page, breakdown
fields and open IP all live in the URL, so any view can be linked and Back works.

## Verification

`go test ./internal/crowdsec/...` covers decoding, de-duplication, the rollup,
late alerts, filters, raw and rollup agreement, owner isolation and pruning.
`go test ./cmd/certainstats-collector/...` runs the collector against a fake
Local API: backfill, JWT reuse and renewal, restarts without duplicates, window
splitting, an unreachable Local API, `--check` and resync.
`node --test web/tests/crowdsec.test.cjs` covers the URL state, the headline and
the page's filtering, sorting, row details, IP view and range controls.
