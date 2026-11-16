# Collectors

A collector is a small program, `certainstats-collector`, installed on a host to
send CertainStats data from software running there. What it sends is a **type**.
The types are [CrowdSec](crowdsec.md) and [Knot DNS](knot.md). Collectors are separate
from agents: an agent reports a machine's own metrics, a collector reports what
other software on it sees. A collector can be linked to the node it runs on, but
it never appears in the Agent Hub, on public dashboards or in node counts.

Collectors are private and owner scoped. They are managed on the Collectors
page (`/collectors`, in the main navigation), which the CrowdSec and Knot DNS
pages link to.

## How it connects

The collector dials out and keeps one WebSocket open to
`<panel>/api/collectors/connect`, so the host needs no open port. It sends its
token in `X-Token` and its version in `X-CertainStats-Collector`. A wrong,
reset or removed token gets HTTP 401 before the upgrade. Connection attempts are
limited to 30 per minute per IP.

The collector keeps no state. Anything it needs to resume, such as a cursor,
lives on the server and is sent on connect, so a restart or an outage only
delays data, and reinstalling loses nothing. It reconnects with jittered backoff
up to five minutes.

Each collector has one live connection; a new one replaces the old. Resetting
the token, removing the collector, or enabling or disabling one of its types
closes the connection, and the collector reconnects to pick up the change. The
server drops a connection that sends nothing for 90 seconds. At most 256
collectors can be connected at once.

## Protocol

Every frame is a CBOR envelope `{type, body}`, at most 1 MiB in either
direction. Message types without a prefix belong to the collector itself. Each
collector type owns a prefix, and the server routes frames by it; CrowdSec uses
`cs_` and Knot DNS `kn_`. Frames for a type that is not enabled for the collector, and unknown
messages, are ignored, so a newer collector still works with an older server.

| Direction | Message | Content |
| --- | --- | --- |
| server → collector | `hello` | Collector ID and name, the types enabled for it, keepalive interval |
| collector → server | `ping` | Collector version and the types switched on locally (each by its switch); sent on connect and every 30 seconds |
| server → collector | `pong` | Answers each `ping`, so both directions carry traffic on a quiet host; the collector also drops a connection silent for 90 seconds |

The rest of the exchange belongs to the types. Wire types live in
`internal/collector/wire`, imported by the server and the binary.

## Setting one up

1. In CertainStats, open the Collectors page, choose **Add collector**, give it
   a name, optionally the node it runs on, and tick the types it collects. The
   install dialog that agents use opens with the Hub URL, the token and setup
   steps for systemd and Docker Compose. **Install** on the collector's row opens
   it again at any time; resetting the token opens it with the new token.
   `/collectors?add=crowdsec` opens the add form with CrowdSec ticked.
2. The panel URL must be reachable from the host. A reverse proxy in front must
   pass WebSocket upgrades and keep idle connections open longer than 30
   seconds, as Beszel agents already need.
3. Install the collector, from the `certainstats-collector-linux-<arch>` release
   files or its own image, `ghcr.io/minoplhy/certainstats-collector`. The image's
   entrypoint is the collector, so flags pass straight through, such as
   `docker compose run --rm certainstats-collector --check`.
4. Configure it through its environment:

   | Variable | Meaning |
   | --- | --- |
   | `HUB_URL` | The panel URL, such as `https://stats.example.com` |
   | `TOKEN` | The token from step 1 |
   | Each type's switch | Switches a type on: `COLLECT_<TYPE>=true` unless the type names its own. CrowdSec uses `COLLECT_CROWDSEC=true`; Knot DNS must be switched on explicitly with `ENABLE_KNOT_DNS=true` (`COLLECT_KNOT` does nothing). With none, the collector exits and names every switch |

   Each type adds its own settings, prefixed with its name. Some are switches
   for optional reading that is off by default, such as CrowdSec's
   `CROWDSEC_HEALTH` or Knot's `KNOT_DNSTAP`.
5. Keep the host clock correct (NTP); data and cursors are timestamps.

`certainstats-collector --check` tests each prerequisite and names the first
that fails. `--once` runs one collection pass and exits. `--version` prints the
version.

The page lists one row per collector and shows where it stands, per type: "Waiting for first
connection", "Offline", "Connected, CrowdSec not switched on
(COLLECT_CROWDSEC=true)" (each type names its own switch), "Connected, <error>", or "Reporting".

## Private API

Paths are relative to the panel's `/api`. Every route except `connect` needs a
signed-in session; mutations need the CSRF token.

| Method | Path | Behaviour |
| --- | --- | --- |
| GET | `/collectors/connect` | Collector WebSocket |
| GET | `/collectors` | Collectors with live status and each type's state, plus the available types; each type carries its `switch` |
| POST | `/collectors` | Create with `name`, optional `agent_id` and `types`; returns the token |
| GET, PATCH, DELETE | `/collectors/{id}` | Read; change `name` or `agent_id`; remove |
| GET | `/collectors/{id}/install` | Install steps with the token, as provision messages (the format of `/agent/install/{id}`) |
| POST | `/collectors/{id}/token` | Replace the token and disconnect |
| PUT, DELETE | `/collectors/{id}/<type>` | Enable or disable a type for this collector |
| … | `/collectors/{id}/<type>/…` | The type's per-collector routes |
| … | `/collectors/<type>/…` | The type's data across the owner's collectors |

Collector IDs start with `col_`, so they never collide with `connect` or a type name.

## Storage

The main SQLite database holds `collectors` (name, the token and its SHA-256
hash, linked node, first and last connection, version) and `collector_types` (one row
per type a collector may send: enabled, whether the host has it switched on,
status, last error, and the type's small JSON state). Removing a collector keeps
its row, with a token that can never match, so data it already sent keeps its
name. Each type keeps its own data where it suits it.

Time series from collectors go to their own Prometheus TSDB, `DATA_DIR/collector_tsdb`,
never to the agent TSDB. A busy collector can neither evict agent metrics nor take
agent query slots: `internal/collector/tsdb` owns the database and its own read
limiter (8 concurrent reads; a read that waits 2 seconds for a slot gets 503). Its
size is set by `COLLECTOR_RETENTION_DURATION` (default 90 days) and
`COLLECTOR_STORAGE_MAX_BYTES`, independent of the agent TSDB's settings.

The token is kept, as agent tokens are, so the install steps can be shown again;
connections are matched by the hash. Collectors created before tokens were kept
show a placeholder and a note to reset the token.

A type adds its own install steps (settings, host commands, Docker mounts, a
note) by implementing `collector.Setup`.

## Adding a type

1. Server: a package under `internal/collector/type/<name>` implementing `collector.Type` (`internal/collector/type.go`):
   a name, label and prefix, connect, message and disconnect handlers, and its
   two route trees. Register it in `cmd/certainstats/main.go`. Time series go to
   the collector TSDB (`internal/collector/tsdb`).
2. Binary: a package under `internal/collectoragent/type/<name>` implementing
   `module.Module` (`internal/collectoragent/module`), listed in `Available()`
   in `internal/collectoragent/config.go`. Its switch is `COLLECT_<NAME>`, unless
   both the module and the server type implement `Switch()` (`module.Switcher`,
   `collector.Switcher`) to name another, as Knot DNS does with `ENABLE_KNOT_DNS`.
   `cmd/certainstats-collector` is only the entrypoint (flags, then `Run`,
   `Once` or `Check`).
3. Setup steps: implement `collector.Setup` on the server type
   (`internal/collector/setup.go`); without it the steps only set the switch.
4. Its own page and docs.

## Verification

`go test ./internal/collector/...` covers tokens, routing by prefix, ignored and
disabled types, oversized frames, connection replacement, token reset, removal
and owner isolation. `go test ./internal/collectoragent/...` runs the real
collector against a fake CrowdSec Local API, a fake Knot control socket and a
real server. `go test ./internal/collector/tsdb/...` covers the collector TSDB
and its limiter.
