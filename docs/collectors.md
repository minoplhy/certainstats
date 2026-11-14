# Collectors

A collector is a small program, `certainstats-collector`, installed on a host to
send CertainStats data from software running there. What it sends is a **type**.
CrowdSec is the first type; see [CrowdSec](crowdsec.md). Collectors are separate
from agents: an agent reports a machine's own metrics, a collector reports what
other software on it sees. A collector can be linked to the node it runs on, but
it never appears in the Agent Hub, on public dashboards or in node counts.

Collectors are private and owner scoped. They are managed from the Collectors
dialog, opened from the CrowdSec page.

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
`cs_`. Frames for a type that is not enabled for the collector, and unknown
messages, are ignored, so a newer collector still works with an older server.

| Direction | Message | Content |
| --- | --- | --- |
| server → collector | `hello` | Collector ID and name, the types enabled for it, keepalive interval |
| collector → server | `ping` | Collector version and the types switched on locally; sent on connect and every 30 seconds |

The rest of the exchange belongs to the types. Wire types live in
`internal/collector/wire`, imported by the server and the binary.

## Setting one up

1. In CertainStats, open the Collectors dialog, choose **Add collector**, give it
   a name, optionally the node it runs on, and tick the types it collects. The
   token is shown once, with setup steps for systemd and Docker Compose.
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
   | `COLLECT_<TYPE>=true` | Switches a type on, such as `COLLECT_CROWDSEC=true`. With none, the collector exits and says so |

   Each type adds its own settings, prefixed with its name.
5. Keep the host clock correct (NTP); data and cursors are timestamps.

`certainstats-collector --check` tests each prerequisite and names the first
that fails. `--once` runs one collection pass and exits. `--version` prints the
version.

The dialog shows where each collector stands, per type: "Waiting for first
connection", "Offline", "Connected, CrowdSec not switched on
(COLLECT_CROWDSEC=true)", "Connected, <error>", or "Reporting".

## Private API

Paths are relative to the panel's `/api`. Every route except `connect` needs a
signed-in session; mutations need the CSRF token.

| Method | Path | Behaviour |
| --- | --- | --- |
| GET | `/collectors/connect` | Collector WebSocket |
| GET | `/collectors` | Collectors with live status and each type's state, plus the available types |
| POST | `/collectors` | Create with `name`, optional `agent_id` and `types`; returns the token once |
| GET, PATCH, DELETE | `/collectors/{id}` | Read; change `name` or `agent_id`; remove |
| POST | `/collectors/{id}/token` | Replace the token and disconnect |
| PUT, DELETE | `/collectors/{id}/<type>` | Enable or disable a type for this collector |
| … | `/collectors/{id}/<type>/…` | The type's per-collector routes |
| … | `/collectors/<type>/…` | The type's data across the owner's collectors |

Collector IDs start with `col_`, so they never collide with `connect` or a type name.

## Storage

The main SQLite database holds `collectors` (name, SHA-256 hash of the token,
linked node, first and last connection, version) and `collector_types` (one row
per type a collector may send: enabled, whether the host has it switched on,
status, last error, and the type's small JSON state). Removing a collector keeps
its row, with a token that can never match, so data it already sent keeps its
name. Each type keeps its own data where it suits it.

## Adding a type

1. Server: a package implementing `collector.Type` (`internal/collector/type.go`):
   a name, label and prefix, connect, message and disconnect handlers, and its
   two route trees. Register it in `cmd/certainstats/main.go`.
2. Binary: a package implementing `module.Module`
   (`cmd/certainstats-collector/module`), listed in `available()` in
   `cmd/certainstats-collector/main.go`. Its switch is `COLLECT_<NAME>`.
3. Setup steps: an entry in `TYPE_SETUP` in
   `web/src/js/components/collectors/setup.js`.
4. Its own page and docs.

## Verification

`go test ./internal/collector/...` covers tokens, routing by prefix, ignored and
disabled types, oversized frames, connection replacement, token reset, removal
and owner isolation. `go test ./cmd/certainstats-collector/...` runs the real
collector against a fake CrowdSec Local API and a real server.
