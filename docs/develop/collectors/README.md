# Collector internals

This page covers the collector wire protocol, private API, storage and how to add a collector type. It is for developers. For what collectors are and how to set one up, see [collectors](../../operate/collectors/README.md).

## Code layout

| Path | What it is |
| --- | --- |
| `internal/collector` | Server side: connection handling, registry of types, alert rounds |
| `internal/collector/wire` | Wire types shared by the server and the collector binary |
| `internal/collector/tsdb` | The collector TSDB and its read limiter |
| `internal/collector/type/<name>` | One server package per type |
| `internal/collectoragent` | The collector binary's logic |
| `internal/collectoragent/type/<name>` | One collector-side module per type |
| `cmd/certainstats-collector` | Entrypoint only: flags, then `Run`, `Once` or `Check` |

## Connection

- The collector opens a WebSocket to `<panel>/api/collectors/connect`.
- It sends its token in `X-Token` and its version in `X-CertainStats-Collector`.
- A wrong, reset or removed token gets HTTP 401 before the upgrade.

## Protocol

Every frame is a CBOR envelope `{type, body}`, at most 1 MiB in either direction.

- Message types without a prefix belong to the collector itself.
- Each collector type owns a prefix, and the server routes frames by it: CrowdSec uses `cs_`, Knot DNS `kn_`.
- Frames for a type that is not enabled for the collector, and unknown messages, are ignored. A newer collector therefore still works with an older server.

| Direction | Message | Content |
| --- | --- | --- |
| server → collector | `hello` | Collector ID and name, the types enabled for it, keepalive interval |
| collector → server | `ping` | Collector version and the types switched on locally (each by its switch). Sent on connect and every 30 seconds. |
| server → collector | `pong` | Answers each `ping`, so both directions carry traffic on a quiet host. The collector also drops a connection silent for 90 seconds. |

The rest of the exchange belongs to the types: see [Knot DNS internals](knot.md#messages) and [CrowdSec internals](crowdsec.md#messages).

## Private API

Paths are relative to the panel's `/api`. Every route except `connect` needs a signed-in session; changes also need the [CSRF token](../../operate/security.md#scripts-and-api-clients-csrf).

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

The main SQLite database holds two tables:

- **`collectors`**: name, the token and its SHA-256 hash, linked node, first and last connection, version.
- **`collector_types`**: one row per type a collector may send, with:
  - whether it is enabled;
  - whether the host has it switched on;
  - status and last error;
  - the type's small JSON state.

Removing a collector keeps its row, with a token that can never match.

Time series go to `internal/collector/tsdb`, which owns the collector TSDB and its own read limiter (8 concurrent reads, 503 after waiting 2 seconds).

### Alert rules and incidents

Alert rules of every collector type share the main database.

- A rule keeps what it watches in `alert_subjects`: source, collector, scope, name.
- Its incidents are `alert_history` rows:

  | Column | Holds |
  | --- | --- |
  | `subject_kind` | The source (type name) |
  | `agent_id` | The collector |
  | `subject_id` | The subject |
  | `subject_snapshot` | What fired, in words |

- The `collector_incident_open` index enforces one open incident per rule and subject.
- Older CrowdSec-only rules (`alert_crowdsec_subjects`) are moved on upgrade.

## Adding a type

1. **Server.** Add a package under `internal/collector/type/<name>` implementing `collector.Type` (`internal/collector/type.go`): a name, label and prefix; connect, message and disconnect handlers; and its two route trees. Register it in `cmd/certainstats/main.go`. Time series go to the collector TSDB (`internal/collector/tsdb`).
2. **Binary.** Add a package under `internal/collectoragent/type/<name>` implementing `module.Module` (`internal/collectoragent/module`), and list it in `Available()` in `internal/collectoragent/config.go`. Its switch is always `COLLECT_<NAME>`.
3. **Setup steps.** Implement `collector.Setup` on the server type (`internal/collector/setup.go`) to add settings, host commands, Docker mounts and a note to the install steps. Without it, the steps only set the switch.
4. **Its own page.** Add `web/templates/collector_types/<name>.html`. The renderer finds it there and the panel serves it at `/collectors/<name>` for every registered type, so nothing else lists it.
5. **Alert rules**, if the type has any:
   - Register a source with `alert.RegisterSubjectSource` (name = type name, label, the scopes its subjects may have besides `all`, and an empty-state hint), and its conditions with `alert.RegisterSubjectTrigger`.
   - Implement `collector.AlertSource`, so the Alerts page can offer, per collector, what to watch.
   - Evaluate on the central timer with a `collector.Round` (`internal/collector/alerts.go`). It loads the type's rules and open incidents, takes one `collector.Verdict` per rule and subject, opens and resolves incidents, and closes the ones nothing kept.
   - Wire the type's `Rules`, `Deliver` and `Tick` in `cmd/certainstats/main.go`.
6. **Docs:** an operator page at `docs/operate/collectors/<name>.md` and an internals page at `docs/develop/collectors/<name>.md`, both listed in `docs/README.md`.

## Tests

- `go test ./internal/collector/...`: tokens, routing by prefix, ignored and disabled types, oversized frames, connection replacement, token reset, removal and owner isolation.
- `go test ./internal/collectoragent/...`: runs the real collector against a fake CrowdSec Local API, a fake Knot control socket and a real server.
- `go test ./internal/collector/tsdb/...`: the collector TSDB and its limiter.

See [testing](../testing.md) for running the whole suite.
