# CrowdSec internals

This page covers how the CrowdSec collector type works inside: Local API reads, health reads, messages, storage, the check registry, the API and tests. It is for developers. For setup and what the page shows, see [CrowdSec](../../operate/collectors/crowdsec.md).

## Reading alerts from the Local API

- The collector logs in with `POST /v1/watchers/login` and lists alerts with `GET /v1/alerts`, every decision attached.
- It sends `include_capi=false`, as `cscli alerts list` does.
- Alerts are found by their start time. `since` and `until` are sent as durations before now; if a Local API rejects that form, the collector switches to RFC 3339 timestamps.
- **Overlap.** Each pass starts an hour before the cursor, because a long bucket can be created well after it started. The server ignores alerts it already has, and the collector skips alerts it already sent on the same connection.
- **Window splitting.** A window that fills a page of 1,000 alerts is split until it doesn't, so nothing is skipped.
- **Decision expiry.** The Local API reports a decision's time left when it is read; the collector turns that into an expiry time.
- **Batches** are split by encoded size as well as count, so each fits one 1 MiB frame.

**To confirm on a real host.** These details were taken from CrowdSec's API specification: how `since` is matched, and the default page size.

## Health reads

Only with `CROWDSEC_HEALTH=true`. SQLite is opened with `mode=ro` and `query_only`.

| Part | Table | Columns | Used for |
| --- | --- | --- | --- |
| `machines` | `machines` | `machine_id`, `last_heartbeat`, `version`, `ip_address`, `is_validated`, `osname`, `osversion` | Is the CrowdSec agent on that machine running |
| `bouncers` | `bouncers` | `name`, `last_pull`, `type`, `version`, `ip_address`, `revoked` | Is the bouncer fetching decisions |
| `metrics` | `metrics` (CrowdSec 1.6.3+) | Each bouncer's newest usage metrics | What the bouncer says it holds and drops |
| `decisions` | `decisions` | A count of active, not simulated `ban` decisions on IPs and ranges, by origin | The other side of that comparison |

- A table or column that does not exist (the `metrics` table before 1.6.3) only drops that part.
- In the bouncer metrics, `active_decisions` per origin is the nftables set or `ipset list` entry count; `dropped` and `processed` are rule counters.

**To confirm on a real host.** Reading a WAL-mode SQLite database through a read-only mount; the PostgreSQL and MySQL paths (tested here against SQLite only); and the metrics payload of bouncers other than the firewall bouncer.

## Messages

Frames use the [collector protocol](README.md#protocol) with the `cs_` prefix.

| Direction | Message | Content |
| --- | --- | --- |
| server → collector | `cs_config` | Cursor, poll interval (60 s), batch size (200), overlap (3,600 s), health read interval (60 s). Sent after `hello`. |
| collector → server | `cs_status` | Local API reachable or its error, machine login, Local API URL. Sent when it changes. |
| collector → server | `cs_alerts` | Batch ID and up to 200 alerts |
| server → collector | `cs_ack` | Batch ID, stored, duplicates, cursor; or an error and when to resend |
| server → collector | `cs_resync` | Read again from a given time |
| collector → server | `cs_health` | Only with `CROWDSEC_HEALTH=true`: the parts read, active decisions by origin, and every machine and bouncer; or the error that stopped the read |

- The collector sends the next batch only after the previous `cs_ack`.
- A batch over 200 alerts closes the connection.
- If storing fails, the server answers with an error and the cursor stays put.
- The cursor is the newest alert start time stored for the collector, and only moves forward.

## Storage

Alerts live in their own SQLite file, `DATA_DIR/crowdsec.db`, so long queries never hold up agent ingestion or the main database.

| Table | Holds |
| --- | --- |
| `alerts` | One row per alert, unique on owner and CrowdSec's alert `uuid`, so a repeated delivery stores nothing. An alert without a uuid gets a stable one from its collector, Local API ID and creation time. The alert's time is its `start_at`, never when it arrived. `action` lists the distinct decision types (`ban`, `ban+captcha`) or `none`. |
| `decisions` | Every decision of every alert, with its expiry |
| `rollup_hourly` | Alert, event and decision counts per owner, hour, scenario, country, machine and action, written in the same transaction as each new alert |
| `components` | Health: one row per machine or bouncer of a collector, with its signals by source, current findings, check streaks (consecutive failing reports) and whether it is muted |
| `collector_health` | Health, per collector: the last report's parts, error and active decisions |

- The `rollup_hourly.decisions` column was added later (schema version 1, in `PRAGMA user_version`). On upgrade it is filled from the raw alerts still kept, so hours whose raw alerts were already pruned count 0 decisions.
- Removing a collector removes its `components` and `collector_health` rows.
- Alert rules keep their machines and bouncers in the main database (`alert_subjects`, source `crowdsec`, scopes `all`, `machine`, `bouncer` and `group`). Incidents are `alert_history` rows with `subject_kind` `crowdsec`, as for [every collector type](README.md#alert-rules-and-incidents). Evaluation runs through the shared `collector.Round`.

### Record engine (`CROWDSEC_ENGINE=rblk`)

`Store.UseRecords(dir)` moves `alerts` and `decisions` into an [rblk](../storage-formats.md) store and stops using `rollup_hourly`; only `components` and `collector_health` are still used in crowdsec.db. `cmd/certainstats` calls it with `DATA_DIR/crowdsec-rblk` when `CROWDSEC_ENGINE=rblk`.

- **Declaration.** `rblkstore.go` declares the schema (`rbSchema`):
  - repeated strings are Dict columns, and `value` is stored as 0 when it equals `ip`
  - the message is kept only when the template `alertMessage` ("Ip X performed 'S' (N events over T)") doesn't rebuild it
  - `uuid` is the dedupe column
  - decisions are child rows, with `until` as the active column and `value` stored as 0 when it equals the alert's

  `ip` and `scenario` are indexed in the head, the same columns crowdsec.db indexes.
- **Reads.** Each `Store` method keeps its signature and branches only where it reads raw alerts: `Alerts`, `Alert`, `IP`, the raw branch of `Summary`, `Timeline` and `Groups`, active decisions, and the oldest alert for coverage (`rblk.Store.Oldest`). `coverage()` reports no rollup and `plan()` always returns `raw`, with no `detail_expired_before`, so every range reads the records. History lasts as long as raw retention; that is the intended trade for having one store.
- **Writes.** `Insert` stores the batch in rblk, which reports duplicates by uuid. It is the only write, so a resend after a crash just reports duplicates.
- **Maintenance.** `Type.Maintain` calls `Store.Seal`, which seals settled hours into blocks, before `Prune`. `Prune` applies raw retention and account removal to rblk too, and the kept-row count in the log adds both stores.

### Which table answers

Every read needs a time range. This section describes the `sql` engine; the record engine always reads records.
- A filter on IP, range, ASN or collector, or a search, reads raw alerts.
- Otherwise, ranges longer than seven days, or older than the retained raw alerts, read `rollup_hourly`.
- When detail is asked for a period whose raw alerts were pruned, responses carry `detail_expired_before`.

## Check registry

A check is one entry in a registry in `internal/collector/type/crowdsec/checks.go`. Each entry has:
- its name and the kind it applies to;
- its parameters, with limits and defaults;
- the parameter a rule's threshold sets;
- the parts it needs;
- a function from signals to a verdict.

Registering a check also registers its rule condition, so it appears on the Alerts page. Signals are stored by source, so a later source, such as a collector on a bouncer's host reading its log, can add its own signal set and a check without changing the existing ones.

## Private API

Paths are relative to the panel's `/api`.

| Method | Path | Behaviour |
| --- | --- | --- |
| GET | `/collectors/{id}/crowdsec` | The collector's CrowdSec status, cursor and machine |
| POST | `/collectors/{id}/crowdsec/resync` | Ask the connected collector to read again from `since_ms` |
| GET | `/collectors/crowdsec/summary` | Alerts, events, unique IPs, active decisions by type, and coverage |
| GET | `/collectors/crowdsec/timeline` | `[bucket start, alerts, decisions]` per bucket, at most 1,000 buckets, zero-filled. Decisions are those the bucket's alerts issued. |
| GET | `/collectors/crowdsec/groups/{field}` | Top values for one field; `limit` 1 to 100 |
| GET | `/collectors/crowdsec/alerts` | Paged alerts: `page`, `limit` (25, 50, 100), `sort`, `order` |
| GET | `/collectors/crowdsec/alerts/{id}` | One alert with all decisions |
| GET | `/collectors/crowdsec/ips/{ip}` | First and last seen, totals, scenarios, machines and latest 50 decisions over retained alerts |
| GET | `/collectors/crowdsec/components` | Every machine and bouncer with its state, findings, firing rules, and for bouncers the decisions held and active by origin; per collector whether health is on, off or failing |
| PUT, DELETE | `/collectors/crowdsec/components/{id}/mute` | Mute or unmute one machine or bouncer |
| GET | `/collectors/crowdsec/subjects` | What a rule can watch, per collector: machines, shared bouncer keys with their entries, single bouncers |

### Parameters

- **Range:** `start` and `end` (Unix ms, at most 20 years apart; `maxSpanMs` in `filter.go`), or `hours`. This is wider than the shared two-year chart range because hourly counts can be kept forever.
- **Filters:** `ip`, `range`, `scenario`, `country`, `machine`, `asn`, `action` and `collector`.
  - `field=value` includes, `not_field=value` excludes.
  - Repeat a parameter for several values. Values are never split on commas.
- **Search:** `q` searches IP, scenario, machine, country, network and range, with wildcard characters taken literally.
- **Time zone:** `tz_offset` (minutes east of UTC) aligns day buckets. The browser labels every timestamp itself.
- A failed query returns an error, never zeros.

## Tests

- `go test ./internal/collector/type/crowdsec/...` covers:
  - decoding, de-duplication, the rollup, late alerts, filters, raw and rollup agreement, owner isolation and pruning;
  - the record engine: `TestRblkMatchesSQL` feeds the same alerts to a crowdsec.db store and an rblk store and compares every screen, coverage included, through sealing, duplicates, late alerts, retention and a removed account (about 10 s). Ranges over seven days are compared too: crowdsec.db answers them from `rollup_hourly`, rblk from its records, and on fully kept data they must agree. `TestRblkIndependent` checks that switching an existing crowdsec.db shows none of its history, even on long ranges, and that rblk writes no rollup rows;
  - for health: each check, including siblings on a shared key; what is not judged; Alerts page rules firing and recovering once with their webhook; incidents closing quietly (muted, removed, rule disabled); and the routes.
- `go test ./internal/collectoragent/...` runs the collector:
  - against a fake Local API: backfill, JWT reuse and renewal, restarts without duplicates, window splitting, an unreachable Local API, `--check` and resync;
  - against a SQLite file shaped like CrowdSec's database: nothing read without the switch, every part, an older schema, a missing file, and the file left untouched.
- `node --test web/tests/crowdsec.test.cjs` covers the URL state, the headline, the page's filtering (including the + Filter popover), sorting, row details, IP view, range controls, the two timeline series and the Collectors page.
