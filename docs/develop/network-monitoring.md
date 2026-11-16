# Network monitoring internals

This page covers how network monitors work inside: agent metadata and capabilities, configuration sync, ingestion, the API, live feeds and caching. It is for developers. For what operators see, see [network monitoring](../operate/network-monitoring.md).

## Agent metadata

- `agents.agent_version` stores the software version as an opaque string, for every provider.
- `protocol_version` is separate:
  - LTstats reports its wire-format version, not a software release;
  - HetrixTools reports its JSON version;
  - Beszel reports its handshake and stats version.
- A missing value keeps the previous observation; a reported downgrade replaces it. Observation time and source are kept.

The private agents API groups this metadata under `extensions.runtime`: `agent_version`, `protocol_version`, `agent_version_source`, `agent_version_observed_at` and `capabilities`.
- These fields are not in the top-level overview.
- Unknown versions are `null`; a missing source and observation time are omitted.
- Identity, status, hardware and traffic fields stay at the top level.
- UI code treats missing extensions as unknown metadata.

## Capabilities

- The parser registry resolves named capabilities to `supported`, `unsupported` or `unknown`, with a reason code and explanation.
- Only the Beszel adapter applies semantic-version rules: basic monitoring needs 0.20.0; custom DNS servers and certificate details need 0.21.0.
- LTstats and HetrixTools have no network adapter yet.
- A new provider implements `ResolveCapabilities` and the `networkmonitor.Adapter` interface, without adopting Beszel's version scheme or CBOR protocol.
- Explicit capability declarations can restrict what an adapter supports.

## Configuration and sync

- Each monitor belongs to one owner and one agent, with a stable random ID. Creating the same configuration on several agents creates independent monitors in one transaction.
- On create, omitting `interval_seconds` or sending 0 selects the 60-second default. An explicit value outside 60–3600 returns 400.
- The interval controls agent probing only. Pull scheduling and the 60-second cache allowance are separate.
- An identity change (target, protocol, TCP port or DNS server) archives the old monitor, creates a new ID, and moves alert membership.
- SQLite stores the desired and the acknowledged configuration generation. Edits made while the agent is offline stay pending.
- **Beszel sync** uses WebSocket action 7: incremental upsert and delete, and a full replacement on reconnect. A full sync after an incremental update reconciles concurrent changes.
- Requests carry correlated IDs and bounded deadlines. Only responses to tracked GetData requests enter host telemetry ingestion.

## Ingesting results

- Probe results arrive in Beszel CombinedData CBOR key 6.
- A SQLite journal entry is written before the TSDB write. Then the latest snapshot is committed and the journal entry removed.
- On startup, pending entries are replayed with their original timestamps.
- Duplicates are suppressed using the connection epoch, the probe timestamp and the lifetime sample count. Older observations within one connection are discarded.
- Certificate details persist when a later packet omits them, with their own observation timestamp.
- Storage keeps integer microseconds, from the normalised adapter contract. API response times are in milliseconds; unavailable latency is `null`.
- Bucket averages weight response sums by successful probe counts. Loss weights failed probes by reported attempt counts. Missing observations, and latency when every probe failed, are `null`.

## Private API

Paths are relative to the panel's `/api`.

| Method | Path | Behaviour |
| --- | --- | --- |
| GET | `/network-monitors` | Paged list; `agent_id`, `q`, `target`, `target_exact`, `protocol`, `state`, `page`, `limit` |
| GET | `/network-monitors/targets` | Alphabetical distinct targets in `items`, restricted by owner, `agent_id`, `protocol` and `state`; independent of paging and target search |
| GET | `/network-monitors/group?target=…` | All current members for an exact target, plus an opaque revision |
| PATCH | `/network-monitors/group` | Edit shared configuration and selected monitors in one transaction |
| POST | `/network-monitors` | Create for `agent_ids` with target, protocol, port, interval and optional DNS server |
| GET | `/network-monitors/{id}` | Configuration, state, latest reading and sync status |
| PATCH | `/network-monitors/{id}` | Edit configuration or change `enabled` |
| DELETE | `/network-monitors/{id}` | Archive and remove from the agent |
| GET | `/network-monitors/{id}/history` | One monitor's historical response time and failure rate |

### Lists and filters

- Lists return up to 100 rows.
- `target` matches stored targets case-insensitively, including hostnames inside URLs. `q` searches target or node. SQL wildcard characters in search text are taken literally.

### Errors

- Changes validate every selected agent's capabilities. A capability failure returns 400 with structured agent, feature and reason details.
- A duplicate configuration returns 409, as does an individual write that would give a group conflicting settings or a duplicate target and agent.

### History

- One monitor per request, like `/api/metrics`. GET parameters never carry comma-separated lists.
- The monitor is authorised before cached data is served or the TSDB is read.
- History uses the private query concurrency and range limits, and returns at most 1,000 points.

### Group PATCH

- Body: `original_target`, `revision`, `target`, `protocol`, `interval_seconds`, `agent_ids`, optional `port` and `dns_server`, and optional `enabled` for a group-wide pause or resume.
- At least one monitor must remain.
- `revision` comes from the group GET (`items` and `revision`). A stale revision returns 409, and the client must reload.
- Shared settings and membership commit together; then the affected agents sync.
- Renaming into an existing target group is rejected.
- Schedule changes keep history. Identity changes archive the old monitors and move alert membership to the replacements.
- Inconsistent older groups return 409 rather than picking settings implicitly.

## Live feed

- Agent overview, agent detail and Network share one SPA shell and one authenticated `/api/ws` connection.
- The socket is a read-only feed: the browser sends no application messages.
- Every ten seconds the server sends one combined agent and network pulse, with all of the owner's non-archived monitors and their committed latest readings from SQLite.
- Public dashboard sockets receive agent telemetry only.
- Both JSON and Protobuf distinguish an omitted network pulse (a read failure) from an empty complete snapshot.
- Reconnects and monitor membership changes reconcile visible lists over HTTP.
- Graphs fetch each series with its own history request, in parallel.

## Caching

- Recent history uses a separate 64 MiB LRU portion of the shared realtime cache, keeping complete committed samples for 24 hours.
- Cold or incomplete ranges warm from the TSDB, including known empty periods. Older ranges and evicted samples fall back to the TSDB.
- Successful graph responses live for 60 seconds in the compressed response cache and share its 128 MiB budget. Owner, monitor, requested range and data revision isolate responses.
- Identical builds coalesce, and only actual TSDB reads take query slots.
- Maintenance logs sample-cache hits, misses, evictions and fallbacks (`network_samples`).
- Revoking a node clears its memory caches; archived network history stays in the TSDB.

## Alert evaluation

- `network_loss` rules select monitor IDs rather than agent IDs, and use `>` with a fixed one-hour window and a 0–100% threshold.
- Evaluation needs an online, capable agent, at least three reported lifetime samples, and a fresh reading. Fresh means within three probe intervals, clamped to between three minutes and one hour.
- Each rule and monitor pair has independent state.
- Incident snapshots keep the target, protocol and DNS server alongside the rule and node snapshots.
- Firing and recovery use the durable notification attempt and retry workflow.
- Capability changes are rechecked inside the evaluation transaction, and again before retries or dispatch.

## Tests

- Race checks cover the agent, network service, SQLite and WebSocket packages. See [testing](testing.md).
- **Real Beszel agent:** set `BESZEL_TEST_AGENT_BINARY` to an agent binary and run `go test ./internal/agent -run '^TestRealBeszelNetworkMonitoring$' -v`. It checks version discovery, immediate probes, stats ingestion, configuration acknowledgement and removal against isolated local servers. Without the variable it is skipped.
