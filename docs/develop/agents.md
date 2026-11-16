# Agent internals

This page covers how agents work inside: provisioning, the three drivers and their wire formats, parsing, normalisation, ingestion, the private API, and how to add a driver. It is for developers. For adding and managing agents, see [agents](../operate/agents.md). The overall data flow is in [architecture](architecture.md#data-flow).

## Code layout

| Path | What it is |
| --- | --- |
| `internal/agent` | HTTP and WebSocket handlers, provisioning, install steps, normalisation, ingestion, revocation |
| `internal/agent_parser` | The `AgentParser` interface and the `Telemetry` / `ParsedData` types |
| `internal/agent_parser/{Beszel,LTstats,HetrixTools}` | One parser per driver |
| `internal/agent_parser/registry` | Parser registry: format detection, capabilities, network adapters |
| `internal/agent_data` | ID and token generation |
| `internal/agentmeta` | Provider-neutral runtime metadata and capabilities |
| `internal/base/agent`, `internal/base/response` | Request and response shapes |
| `internal/ws` | Beszel `Hub` (request correlation), connection manager |
| `internal/metrics` | Realtime cache fed by ingestion |

## Identity and provisioning

- **Agent IDs** are `agt_` plus 32 random alphanumeric characters (`agentdata.GenerateAgentID`).
- **Tokens** are random alphanumeric strings: 32 characters for LTstats and HetrixTools (LTstats has a fixed `[33]byte` token field), 64 for Beszel (`GenerateDeviceToken`).
- **Nicknames** default to a random `adjective-noun` (`GenerateNickname`); at most 64 characters.
- **Type** is stored at provisioning (default `beszel`, validated against the registry) and enforced on every report: a payload of another type for that token is rejected.
- **Beszel SSH keys.** Provisioning a Beszel agent generates an Ed25519 key pair (`GenerateAndSaveSSH`) and stores it in `beszel_ssh`, one key per agent. The public key goes in the install steps as the hub key; the private key signs the agent's token during the handshake.
- **Install steps** (`setup_instruction.go`) are returned as provision messages: a tree of `copy`, `big_copy`, `command`, `tabs`/`tab`, `warning` and `note` items that the browser's provision renderer draws. The Hub URL is `scheme://host + panel path`, from `ctx.PanelOrigin` (`PANEL_URL` when set, else the request).

## Drivers

### Beszel (WebSocket, pulled)

The agent connects to `GET /api/beszel/agent-connect` with `X-Token` (at most 64 characters) and its version in `X-Beszel`.

1. **Token check.** An unknown token, or a token belonging to a non-Beszel agent, gets 401 before the upgrade.
2. **Reader.** One goroutine is the only socket reader. Responses are correlated to requests by ID (`ws.Hub.Route`); only responses to tracked `GetData` requests are passed to ingestion.
3. **Handshake.** The hub signs the token with the agent's private key and sends `CheckFingerprint` (action 1) with the signature and `NeedSysInfo`. The agent must answer with a non-empty fingerprint within 10 seconds. An agent without a valid stored key is refused.
4. **Runtime.** The `X-Beszel` version is stored as the first runtime observation (source `beszel_header`).
5. **Registration.** The hub is registered with the connection manager under its token. Each connection gets a random **connection epoch**, used to suppress duplicate network monitor results.
6. **Network sync.** A full network monitor sync runs on every connect, then pending configuration is retried after each stats payload. See [network monitoring internals](network-monitoring.md#configuration-and-sync).
7. **Pulling.** An initial `GetData` (action 0) with details is sent at once. After that, the background routine sends `GetData` to every connected hub every `BESZEL_EVERY` (default `UPDATE_EVERY`, 60 s), with `CacheTimeMs` 60000 and `IncludeDetails` every 30th request.

Requests are CBOR `HubRequest{Action (0), Data (1), Id (2)}`. Other Beszel actions (container logs, SMART, systemd, ZFS, package updates) are defined in `internal/ws/request_manager.go` but unused; action 7 is network monitor sync.

Parsing (`Beszel/parse.go`): stats arrive as CBOR CombinedData. GB float fields are converted to bytes. Network values are cumulative since boot (`IOCumulative`) and disk I/O values are per second (`IORate`). Network monitor results are in CombinedData key 6 and are routed to the network service, not to agent ingestion.

### LTstats (HTTP, binary)

`POST /submit` with a packed little-endian binary body:

| Part | Size | Content |
| --- | --- | --- |
| `NetHeader` | 35 bytes | `Token [33]byte` (32 + NUL), `Flags` (low 7 bits version, high bit "includes details"), `StatsCount` |
| `Details` (if flagged) | 112 bytes | Uptime, Linux version (32), CPU model (48) and their lengths, CPU cores, RAM, swap and disk sizes |
| `StatsT` × `StatsCount` | 40 bytes each | Unix time, CPU / I/O wait / steal / RAM / swap / disk percentages as integer + hundredths bytes, RX and TX bytes, read and written sectors |

- The body length must equal exactly `35 + 40 × StatsCount (+ 112)`.
- RAM, swap and disk used are computed from percentages and the sizes in `Details`. Without details in the payload, those three metrics are marked missing.
- Network and disk values are bytes in the interval (`IODelta`).
- Each sample's `SourceID` is `ltstats:<unix time>`.

### HetrixTools (HTTP, JSON)

`POST /submit` with a form body `j=<base64 gzip JSON>`. Disks, network interfaces, kernel, CPU model and I/O fields inside the JSON are themselves base64.

- Network and disk I/O values are per second (`IORate`): HetrixTools divides by its interval.
- The payload's `Version` is stored as runtime metadata (source `hetrixtools_payload`).
- The sample's `SourceID` is `hetrix:<unix time>` when the payload carries a plausible time; otherwise the server's time is used and there is no `SourceID`.
- Fields that are empty in the payload are marked missing.

### Format detection

`registry.Detect` tries each HTTP parser's `ParseToken`: HetrixTools first (it validates base64 and JSON), then LTstats (binary, and lenient). The first that returns a token wins. Beszel is never detected this way; it has its own route.

## From payload to storage

`SubmitHandler` and the Beszel handler both end in `agent.Ingest`.

1. **Submit only.** Read at most 16 KiB, detect the type, look up the agent by token (401 if unknown), parse, and check the type matches. Disk size is set to the sum of the first sample's partition sizes. Answers `1` on success; 500 with "Ingestion failed; retry" when storage fails, so the agent resends.
2. **Lock.** `Ingest` takes the agent's lock: one of 256 striped mutexes, chosen by FNV-1a hash of the agent ID. All processing for one agent is serialised.
3. **Validate.** Runtime metadata is validated. Sample timestamps must be positive and at most 10 minutes in the future. Rates, percentages and intervals must be finite and non-negative.
4. **Replay first.** Pending journal records for this agent are replayed before the new batch.
5. **Batch ID.** When every sample has a `SourceID`, each sample is its own record with ID `sha256(agentID:SourceID)`, so a resent sample is recognised and skipped. Otherwise the batch gets a random ID (at-least-once).
6. **Normalise** (`normalizeIOMetrics`):
   - A missing timestamp is filled in from now minus the cadence per remaining sample.
   - `IntervalSeconds` comes from the payload, else the gap since the agent's previous sample, else the configured cadence (`UPDATE_EVERY`, then `BESZEL_EVERY`, then the default).
   - `IOCumulative` network values become deltas against the previous reading. The first reading sets a baseline and is marked missing; a counter that went backwards is marked missing.
   - `IORate` values are multiplied by the interval.
   - After this, every byte value is **bytes in the interval** (`IODelta`).
   - The per-agent counter state lives in memory (`ioStates`). It is rolled back if the journal write fails, and dropped when the agent is revoked (`ForgetAgent`).
7. **Journal.** The normalised payload and the lifetime-counter increments (network and per-disk) are written in one SQLite transaction (`IngestionBegin`).
8. **Apply** (`replayIngestion`): write samples to the TSDB, store runtime metadata, upsert hardware details (or just the heartbeat when there are none), mark the record complete, and update the realtime cache.

On startup, `RecoverIngestion` replays every pending record before the server accepts traffic. The journal's retention and cleanup are described in [architecture](architecture.md#ingestion-and-recovery).

### What goes into the TSDB

Every series has `user_id` and `agent_id` labels; disk series also have `path`.

| Series | Value |
| --- | --- |
| `agent_sample_interval_seconds` | The sample's duration |
| `agent_cpu_usage`, `agent_cpu_iowait`, `agent_cpu_steal` | Percent |
| `agent_ram_used`, `agent_swap_used` | Bytes |
| `agent_disk_used` | Bytes, per path |
| `agent_disk_usage` | Percent, per path (NaN when the size is unknown) |
| `agent_disk_read_bytes`, `agent_disk_write_bytes` | Bytes in the interval, per path |
| `agent_rx_bytes`, `agent_tx_bytes` | Bytes in the interval; not written when network data is missing |

A metric the parser marked missing is written as NaN, so charts show a gap. Rates (bytes per second) are worked out at read time from the interval bytes and `agent_sample_interval_seconds`.

### Realtime cache

`metrics.RealtimeCache` holds each agent's latest snapshot, for the live feed and the Agent Hub, plus 24-hour per-metric windows that serve recent chart ranges without a TSDB read. When the routine marks an agent offline, its snapshot is removed.

## Background routine

`internal/routine` drives agents on timers:

| Timer | Default | Does |
| --- | --- | --- |
| Beszel pull | `BESZEL_EVERY` (= `UPDATE_EVERY`) | `GetData` to every connected Beszel hub |
| UI pulse | 10 s | Pushes snapshots to browsers |
| Main tick | `UPDATE_EVERY` (60 s) | Marks agents offline after 3 minutes without data, evaluates alerts, retries failed notifications |
| Hourly | 1 h | Maintenance, including journal cleanup |

## Private API

Paths are relative to the panel's `/api`. All need a signed-in session; changes also need the [CSRF token](../operate/security.md#scripts-and-api-clients-csrf).

| Method | Path | Behaviour |
| --- | --- | --- |
| GET | `/agents` | The owner's agents: identity, status, hardware, lifetime network and disk counters, and `extensions.runtime` (version, protocol version, source, observed time, capabilities) |
| POST | `/agent` | Provision: `agent_type` (default `beszel`), optional `nickname`. Returns the ID, name and install steps. |
| PUT | `/agent` | Change `nickname` and/or `note` for `agent_id`. Nickname 1–64 characters; note at most 10,000. |
| DELETE | `/agent?agent_id=` | Revoke: delete the agent, close its connection, drop its counter state and cache, invalidate caches, and delete its TSDB series except `network_monitor_*` |
| GET | `/agent/install/{id}` | Install steps with the current token and SSH key |
| GET | `/agent/uninstall/{id}` | Uninstall commands |
| POST | `/agent/reset/token/{id}` | New token; closes the live connection |
| POST | `/agent/reset/ssh/{id}` | Beszel only: new Ed25519 key pair; closes the live connection; returns the public key |
| GET | `/agent/ssh-key/{id}` | The agent's public SSH key |
| GET | `/agents/management` | Management rows, including tokens and public keys |

Agent endpoints outside `/api`: `POST /submit` (LTstats, HetrixTools) and `GET /api/beszel/agent-connect` (Beszel). The server-rendered panel also has form routes (`/agent/provision`, `/agent/rename`, `/agent/reset/token`, `/agent/reset/ssh`, `/agent/delete`).

## Adding a driver

1. **Parser.** Add a package under `internal/agent_parser/<Name>` implementing `AgentParser`: `AgentType()`, `ParseToken(input)` and `Parse(raw)`. Return `ParsedData` with:
   - `Metrics`: one `Telemetry` per sample, with `Timestamp`, a `SourceID` if the source has a stable sample identity, `IntervalSeconds` if known, and `Missing` for metrics it could not read;
   - `NetworkIOType` and `DiskIOType` declaring what byte values mean (`IODelta`, `IORate` or `IOCumulative`); normalisation does the rest;
   - `AgentInfo` when the payload carries hardware details, and `Runtime` for version metadata.
2. **Register** it in `registry.NewRegistry()`.
3. **Transport.**
   - HTTP push to `/submit`: add it to `registry.Detect`, ordered so that stricter formats are tried before lenient ones. The body limit is 16 KiB.
   - Anything else needs its own route and handler that ends in `agent.Ingest`.
4. **Tokens.** If the agent has a fixed-size token field, set the length in `GenerateDeviceToken`.
5. **Install steps.** Add a case to `getSetupInstructions` and `getUninstallInstructions`.
6. **Network monitors** (optional): implement `ResolveCapabilities` and `networkmonitor.Adapter`. See [network monitoring internals](network-monitoring.md#capabilities).
7. **Docs:** add the driver to [agents](../operate/agents.md) and this page.

## Tests

- `go test ./internal/agent/...`: ingestion, journal replay, duplicate suppression, revocation, install steps, the agent list; plus churn tests and goleak checks.
- `go test ./internal/agent_parser/...`: each parser, runtime metadata, Beszel capabilities, format detection.
- The opt-in soak test and the real Beszel agent test are described in [testing](testing.md).
