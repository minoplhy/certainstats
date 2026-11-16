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

CrowdSec can take a while to start, and Compose's `depends_on` only waits for its
container, not its Local API. For the first 2 minutes after the collector
starts, until the Local API first answers, an unreachable Local API is logged
as `waiting for the Local API` and not reported to CertainStats. The collector
retries after 5 seconds, then doubles the wait up to the poll interval. After
that, or once the Local API has answered, an outage is reported at once and
retried the same way. `--once` always reports at once. A Compose healthcheck
with `depends_on: condition: service_healthy` shortens the wait further.

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
Access to CrowdSec's database is not needed either, unless the host switches on
[machine and bouncer health](#machine-and-bouncer-health).

When the Local API is unreachable the collector stays connected and reports the
error, which the Collectors page and the CrowdSec page headline show.

## Machine and bouncer health

A Local API can serve many machines (log processors) and bouncers. This part
shows whether each one is alive, gives a hint when a bouncer may not be applying
decisions, and can notify when one goes quiet and when it comes back.

### The boundary: read-only, and only when the host says so

The Local API has no route that lists machines or bouncers; `cscli` reads
CrowdSec's database for that, and so does the collector. It never changes
anything in CrowdSec:

- It is **off by default**. The collector opens CrowdSec's database only when
  the host sets `CROWDSEC_HEALTH=true`. Without it no database code runs, no
  path is probed, and the panel cannot switch it on.
- It reads with its own fixed `SELECT` statements. It does not use CrowdSec's Go
  client, which changes the database file's permissions and runs a schema
  migration every time it opens.
- SQLite is opened with `mode=ro` and `query_only`, so a missing file is an
  error, never a new file. PostgreSQL and MySQL queries run in read-only
  transactions.
- CrowdSec's `config.yaml` is never read; it holds the read-write credentials.

| Variable | Default | Meaning |
| --- | --- | --- |
| `CROWDSEC_HEALTH` | off | `true` lets the collector read CrowdSec's database |
| `CROWDSEC_DB` | `/var/lib/crowdsec/data/crowdsec.db` | A SQLite path, or `postgres://user:password@host:5432/crowdsec` or `mysql://user:password@host:3306/crowdsec` |
| `CROWDSEC_HEALTH_READ` | all four | Which parts may be read: `machines`, `bouncers`, `metrics`, `decisions`, separated by commas |

In Docker, mount CrowdSec's data directory into the collector read-only
(`crowdsec-db:/var/lib/crowdsec/data:ro`). For PostgreSQL or MySQL, give the
collector its own user with `SELECT` on `machines`, `bouncers`, `metrics` and
`decisions` only; `--check` warns when a PostgreSQL user could write.
`--check` reports health as a line of its own and never fails because of it.

### What is read

| Part | Table | Columns | Used for |
| --- | --- | --- | --- |
| `machines` | `machines` | `machine_id`, `last_heartbeat`, `version`, `ip_address`, `is_validated`, `osname`, `osversion` | Is the agent on that machine running |
| `bouncers` | `bouncers` | `name`, `last_pull`, `type`, `version`, `ip_address`, `revoked` | Is the bouncer fetching decisions |
| `metrics` | `metrics` (CrowdSec 1.6.3+) | each bouncer's newest usage metrics | What the bouncer says it holds and drops |
| `decisions` | `decisions` | a count of active, not simulated `ban` decisions on IPs and ranges, by origin | The other side of that comparison |

A part the host leaves out is never queried, and the checks that need it are
listed on the page as not available. A table or column that does not exist
(the `metrics` table before 1.6.3) only drops that part. Machines and bouncers
are read every minute, the two heavier parts every five. A bouncer's metrics
payload is reduced on the host to three numbers (decisions held per origin,
packets dropped, packets processed); the payload itself never leaves.

### What each signal can prove

All of it is the component's own word, recorded by the Local API:

- A fresh `last_heartbeat` shows the agent runs and reaches the Local API.
- A fresh `last_pull` shows the bouncer runs and fetches decisions. It does not
  show they reached the firewall. A bouncer in live mode only asks when it has
  traffic, so mute it or allow a longer quiet time.
- A bouncer never reports that applying a decision failed; it only logs that on
  its own host. But the firewall bouncer's metrics, pushed every 15 minutes,
  are read from the kernel: `active_decisions` per origin is the real nftables
  set or `ipset list` entry count (0 when `ipset list` fails), and `dropped`
  and `processed` are rule counters. So a firewall holding far fewer decisions
  than the Local API has active is a hint that they are not being applied.
  Sizes cannot prove it, and the reason is only in the bouncer's log.

### Checks

Health is a list of named checks. On the CrowdSec page a check that fails at
its default numbers is a **caution** (amber); it opens no incident, sends
nothing and never changes the headline.

| Check | Applies to | Default numbers | What it can show |
| --- | --- | --- | --- |
| `heartbeat` | machines | quiet for 5 minutes | the agent runs and reaches the Local API |
| `pull` | bouncers | quiet for 60 minutes | the bouncer runs and fetches decisions |
| `held` | bouncers | holds under 50% of at least 20 active decisions of an origin it reports, on 2 metric reports in a row; metrics with no `active_decisions` items at all count as holding 0 of every active origin, once the bouncer is known to hold decisions: it has sent decision counts before, or it counts packets, which only a firewall bouncer does (a bouncer in live mode does neither and is not judged) | it may not be applying decisions |
| `siblings` | bouncers sharing a key | holds under 10% of what its largest sibling holds, that sibling holding at least 1,000, on 2 metric reports in a row; metrics with no decision counts at all (what a failing ipset sends) count as holding 0 | its ipset or nftables set may be failing |

**Shared keys.** When one bouncer key is used from several addresses, CrowdSec
creates one entry per address named `name@ip`. Those entries apply the same
decisions, so `siblings` compares each with the others of its key; one holding
0 while its siblings hold 30,000 is what a firewall bouncer looks like when
`ipset` fails, since it then reports 0 entries. The base name is found by
stripping trailing `@<ip>` parts, as CrowdSec does.

A component is judged only while its collector is connected and its last report
is error-free and younger than five minutes. A collector that died, or a host
that switched health off, therefore never sets off a round of notifications.
Not judged at all: muted components, revoked bouncers, machines that are not
validated, and the collector's own machine login, which never sends a heartbeat.

### Alert rules

Notifications come from ordinary alert rules on the Alerts page. **New rule**
offers a CrowdSec group under **When**, one condition per check
(`crowdsec_heartbeat`, `crowdsec_pull`, `crowdsec_held`, `crowdsec_siblings`),
and in place of **Servers** a **Watch** list per collector, showing the machines
or the bouncers depending on the condition:

- **Every machine and bouncer** of a collector, including ones added later;
- one machine;
- one shared bouncer key: every address using it, new addresses included;
- one bouncer entry.

The rule's threshold sets the check's main number (minutes, or a percentage);
the others keep their defaults, and the check does its own timing, so there is
no operator or duration. A check that needs several reports in a row counts the
reports seen since the rule was created; bouncers send usage metrics every 15
minutes, so a new `held` or `siblings` rule can take up to half an hour to fire.

A firing rule is an incident in the Alerts incident history like any other:
delivery, retry and the message templates are the same (`{{NICKNAME}}` is the
machine or bouncer; `{{COLLECTOR}}`, `{{SUBJECT_KIND}}` and `{{DETAIL}}` are
added). On the CrowdSec page the component turns red and names the rule. An
incident closes without a notification when its rule is disabled, deleted or no
longer covers the component, or when the component is muted, deleted from
CrowdSec, or its collector removed. A collector that cannot be judged leaves its
incidents as they are.

A check is one entry in a registry in `internal/collector/type/crowdsec/checks.go`: its name,
the kind it applies to, its parameters with limits and defaults, the parameter
a rule's threshold sets, the parts it needs and a function from signals to a
verdict. Registering it also registers its rule condition, so it appears on the
Alerts page. Signals are stored by source, so a later source, such as a
collector on a bouncer's host reading its log, adds its own signal set and a
check without changing the existing ones.

To confirm on a real host: reading a WAL-mode SQLite database through a
read-only mount, the PostgreSQL and MySQL paths (tested here against SQLite
only), and the metrics payload of bouncers other than the firewall bouncer.

## Messages

| Direction | Message | Content |
| --- | --- | --- |
| server → collector | `cs_config` | Cursor, poll interval (60 s), batch size (200), overlap (3,600 s), health read interval (60 s); sent after `hello` |
| collector → server | `cs_status` | Local API reachable or its error, machine login, Local API URL; sent when it changes |
| collector → server | `cs_alerts` | Batch ID and up to 200 alerts |
| server → collector | `cs_ack` | Batch ID, stored, duplicates, cursor; or an error and when to resend |
| server → collector | `cs_resync` | Read again from a given time |
| collector → server | `cs_health` | Only with `CROWDSEC_HEALTH=true`: the parts read, active decisions by origin, and every machine and bouncer; or the error that stopped the read |

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
- `rollup_hourly`: alert, event and decision counts per owner, hour, scenario,
  country, machine and action, written in the same transaction as each new
  alert. The `decisions` column was added later (schema version 1, in
  `PRAGMA user_version`); on upgrade it is filled from the raw alerts still
  kept, so hours whose raw alerts were already pruned count 0 decisions.

Health adds two tables to the same file: `components` (one row per machine or
bouncer of a collector, with its signals by source, its current findings, the
streaks of its checks and whether it is muted) and `collector_health` (per
collector: the last report's parts, error and active decisions). Removing a
collector removes both. CrowdSec alert rules keep their machines and bouncers in
the main database (`alert_subjects`, source `crowdsec`, scopes `all`, `machine`,
`bouncer` and `group`), and their incidents are `alert_history` rows with
`subject_kind` `crowdsec`, the collector in `agent_id`, the component in
`subject_id` and what fired in `subject_snapshot`, like every collector type's
(see [collectors](collectors.md#storage)). Evaluation runs through the shared
`collector.Round`.

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
| GET | `/collectors/crowdsec/timeline` | `[bucket start, alerts, decisions]` per bucket, at most 1,000 buckets, zero-filled; decisions are those the bucket's alerts issued |
| GET | `/collectors/crowdsec/groups/{field}` | Top values for one field; `limit` 1 to 100 |
| GET | `/collectors/crowdsec/alerts` | Paged alerts: `page`, `limit` (25, 50, 100), `sort`, `order` |
| GET | `/collectors/crowdsec/alerts/{id}` | One alert with all decisions |
| GET | `/collectors/crowdsec/ips/{ip}` | First and last seen, totals, scenarios, machines and latest 50 decisions over retained alerts |
| GET | `/collectors/crowdsec/components` | Every machine and bouncer with its state, findings, firing rules, and for bouncers the decisions held and active by origin; per collector whether health is on, off or failing |
| PUT, DELETE | `/collectors/crowdsec/components/{id}/mute` | Mute or unmute one machine or bouncer |
| GET | `/collectors/crowdsec/subjects` | What a rule can watch, per collector: machines, shared bouncer keys with their entries, single bouncers |

Data reads take `start` and `end` (Unix ms, at most 20 years apart) or `hours`,
and filters on `ip`, `range`, `scenario`, `country`, `machine`, `asn`, `action`
and `collector`: `field=value` includes, `not_field=value` excludes, and a
parameter is repeated for several values. Values are never split on commas.
`q` searches IP, scenario, machine, country, network and range, with wildcard
characters taken literally. `tz_offset` (minutes east of UTC) aligns day
buckets; the browser labels every timestamp itself. A failed query returns an
error, never zeros.

## The page

`/collectors/crowdsec` (reached from the Collectors page; `/crowdsec` redirects there) opens with a sentence headline and figures: alerts, unique IPs,
active bans now and collectors reporting. **Collectors** links to the
Collectors page. Below is a one-row filter bar:
- search;
- **+ Filter**, the shared filter menu (field, is / is not and value), as on the Knot DNS page;
- one range dropdown holding the quick ranges, a custom range and **All**, a
  range starting at the oldest data.

Once some host has switched health on, the hero shows how many machines and
bouncers there are and how many need a look, and **Machines and bouncers** opens
them in a popup with two lists, laid out like the Management page. A machine's
row opens to its address, version, system, validation and last heartbeat. A
bouncer's row opens to everything it reported: type, version, address, last
pull, when it last sent usage metrics, the decisions it holds per origin next to
the Local API's active count, and packets dropped and processed. A shared key
is one row whose entries sit side by side. Each has **Mute**. What needs a look
comes first, rows update in place every 30 seconds, and an open row stays open.
A firing rule takes over the headline; a caution never does.

Active filters show as removable chips under the bar. Then come:
- the timeline of alerts and the decisions they issued, as two series (brush to zoom);
- four breakdowns whose field can each be changed;
- the alert table.

Clicking a value in a breakdown filters in place, and its second button
excludes it. Table cells have copy and filter buttons, columns sort, and a row
expands to show every decision. Clicking an IP opens its own view. The range,
filters, sort, page, breakdown fields and open IP all live in the URL, so any
view can be linked and Back works.

## Verification

`go test ./internal/collector/type/crowdsec/...` covers decoding, de-duplication, the rollup,
late alerts, filters, raw and rollup agreement, owner isolation and pruning;
for health, each check including siblings on a shared key, what is not judged,
Alerts page rules firing and recovering once with their webhook, incidents
closing quietly (muted, removed, rule disabled), and the routes.
`go test ./internal/collectoragent/...` runs the collector against a fake
Local API: backfill, JWT reuse and renewal, restarts without duplicates, window
splitting, an unreachable Local API, `--check` and resync; and against a SQLite
file shaped like CrowdSec's database: nothing read without the switch, every
part, an older schema, a missing file, and the file left untouched.
`node --test web/tests/crowdsec.test.cjs` covers the URL state, the headline,
the page's filtering (including the + Filter popover), sorting, row details, IP
view, range controls, the two timeline series and the Collectors page.
