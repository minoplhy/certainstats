# Agent network monitoring

Network monitors are managed from the private Go panel's Network page or a node's
details. ICMP, TCP, HTTP/HTTPS and DNS probes run on the selected agent, so targets
are reached from that machine's network. These endpoints are authenticated and
owner scoped; monitor data is not exposed on public dashboards.

## Providers and runtime metadata

`agents.agent_version` stores the software version as an opaque string for every
provider. `protocol_version` is separate: LTstats reports its wire-format version,
not a software release. HetrixTools reports its JSON version; Beszel reports its
handshake and stats version. Missing values preserve the previous observation,
and reported downgrades replace it. Observation time and provenance are retained.

The private agents API always groups this metadata under `extensions.runtime`:
`agent_version`, `protocol_version`, `agent_version_source`,
`agent_version_observed_at`, and `capabilities`. These fields are absent from the
top-level overview. Unknown versions are `null`; missing source and observation
time are omitted. Existing identity, status, hardware and traffic fields remain
at the top level. UI consumers tolerate missing extensions as unknown metadata.

The parser registry resolves named capabilities into `supported`, `unsupported`
or `unknown`, with a reason code and explanation. Only the Beszel adapter applies
semantic-version rules: basic monitoring requires 0.20.0, and custom DNS servers
and certificate details require 0.21.0. LTstats and HetrixTools currently have no
network adapter. Future providers can implement `ResolveCapabilities` and the
`networkmonitor.Adapter` interface without adopting Beszel's version scheme or
CBOR protocol. Explicit capability declarations can restrict adapter support.

## Configuration and synchronization

Each monitor belongs to one owner and one agent, with a stable random ID. Creating
the same configuration on several agents creates independent monitors atomically.
New monitors default to a 60-second probe interval. `interval_seconds` values
must be between 60 and 3600 seconds. On creation,
omitting the interval or sending zero selects the default. Explicit interval
changes outside the supported range return HTTP 400. This setting controls agent probing;
pull scheduling and the 60-second cache allowance are separate and unchanged.
Interval edits retain history. Target, protocol, TCP port or DNS server edits
archive the old monitor, create a new identity, and transfer alert membership.
Pause and archive close active incidents as monitoring removed, preserving history.
Deleting a node archives its monitors.

SQLite stores desired and acknowledged configuration generations. Offline edits
remain pending. Beszel uses WebSocket action 7 for incremental upsert/delete and
full replacement on reconnect. A full synchronization after an incremental update
reconciles concurrent changes. Requests have correlated IDs and bounded deadlines;
only responses to tracked GetData requests enter host telemetry ingestion.

Probe results arrive in Beszel CombinedData CBOR key 6. A SQLite journal precedes
TSDB writes, then the latest snapshot is committed and the journal entry removed.
Startup replays pending entries with their original timestamps. Duplicate results
are suppressed using connection epoch, probe timestamp and lifetime sample count.
Older observations within a connection are discarded.
Certificates persist when a later packet omits certificate details, with a separate
observation timestamp.

## Private API

Paths below are relative to the configured panel's `/api` prefix.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/network-monitors` | Paginated list; `agent_id`, `q`, `target`, `protocol`, `state`, `page`, `limit` |
| POST | `/network-monitors` | Create for `agent_ids` with target, protocol, port, interval and optional DNS server |
| GET | `/network-monitors/{id}` | Configuration, state, latest reading and sync status |
| PATCH | `/network-monitors/{id}` | Edit configuration or change `enabled` |
| DELETE | `/network-monitors/{id}` | Archive and remove from the agent |
| GET | `/network-monitors/{id}/history` | One monitor's historical response time and failure rate |

Lists allow up to 100 rows. Each agent allows up to 100 current monitors. Mutations
validate every selected agent's capabilities and reject duplicate configurations.
Capability failures return HTTP 400 with structured agent, feature and reason
details; duplicate configurations return 409. History is one monitor per request,
like `/api/metrics`; GET parameters never carry comma-separated lists. A history
query authorizes its monitor before serving cached data or reading TSDB, reuses
private query concurrency and range limits, and returns at most 1,000 points.

Storage retains integer microseconds from the normalized adapter contract. Latest
and historical API response times are in milliseconds, with unavailable latency
represented by null. Bucket averages weight response sums by successful probe counts;
loss weights failed probes by reported attempt counts. Missing observations and
latency when all probes fail are null. Beszel can report overlapping windows,
so these charts summarize reported windows, not an exact count of unique probes.

## Live readings and navigation

Agent overview, agent detail and Network use one SPA shell and one authenticated
`/api/ws` connection. Direct links, reloads and Back/Forward keep their existing
URLs. Switching views preserves filters, pagination, selected ranges and zoom;
hidden views pause HTTP refreshes, and node sections remain lazy.

The socket is a read-only feed: the browser sends no application messages.
Every ten seconds the server sends one combined agent/network pulse, with all
of the owner's non-archived network monitors and committed SQLite latest
readings. Public dashboard sockets receive only agent telemetry. JSON and
Protobuf transports distinguish an omitted network pulse (read failure) from an
empty complete snapshot. Reconnect and monitor membership changes reconcile
visible lists through HTTP. Cards fall back to HTTP polling when disconnected or
network pulses are missing for 45 seconds; graphs retain their ten-second cached
HTTP refresh while visible.

## Network graphs

The network page opens with response-time and failed-check graphs above the monitor
list. Shared hostname/target substring, protocol, node and state filters control
both views. `target` matches stored targets case-insensitively, including hostnames
inside URLs; existing `q` still searches target or node. SQL wildcard characters in
search text are treated literally. Protocol accepts `icmp`, `tcp`, `http` or `dns`.

Graphs automatically show the first ten matches in the list's stable ordering,
independently of list pagination. Each series is fetched with its own history
request, in parallel; a failed series shows a retry without hiding the others. A grouped target/protocol legend toggles each
node's series in both graphs, with stable colors. Filters reset series toggles and
preserve the selected time window and zoom; refresh preserves all three. The time
controls share the agent graph's presets, custom Start/End picker and remembered
`certainstats_active_hours` preference, with a six-hour fallback. Brushing either
chart updates both charts and the picker's custom range. The picker's Reset
clears the custom range and returns to the last preset. Node sections load graphs
when opened.

Recent history uses a separate 64 MiB LRU portion of the shared realtime cache,
retaining complete committed samples for 24 hours. Cold or incomplete ranges warm
from TSDB, including known empty periods; older ranges and evicted samples fall
back to TSDB. Successful graph responses live for 60 seconds in the existing
compressed response cache and share its 128 MiB budget. Owner, monitor,
requested range and data revision isolate responses. Identical builds coalesce,
and only actual TSDB reads consume query slots. Maintenance records sample-cache
hits, misses, evictions and fallbacks. Node revocation clears memory caches and
retains archived network history in TSDB.

## Alerts and incidents

`network_loss` rules select monitor IDs rather than agent IDs and use `>` with a
fixed one-hour window and a threshold from 0 to 100 percent. Evaluation requires
an online capable agent, at least three reported lifetime samples and a fresh
reading. Freshness is three probe intervals, bounded between three minutes and
one hour. Stale, offline, unsupported or unknown monitors do not fire or recover.

Each rule/monitor pair maintains independent state. Incident snapshots retain the
target, protocol and DNS server alongside the existing rule and node snapshots.
Firing and recovery use the existing durable notification attempt and retry
workflow. Capability changes are rechecked in the evaluation transaction and
before retries or dispatch. Removing monitoring closes incidents without emitting
a false recovery notification.

## Verification

Install browser test dependencies with `npm ci --prefix web --no-audit --no-fund`.
Run `go test ./...` and `node --test web/tests/*.test.cjs`. Race checks cover the
agent, network service, SQLite and WebSocket packages. To exercise a real Beszel
agent against isolated local servers, set `BESZEL_TEST_AGENT_BINARY` to its binary
and run `go test ./internal/agent -run '^TestRealBeszelNetworkMonitoring$' -v`.
The test verifies version discovery, immediate probes, stats ingestion,
configuration acknowledgement and removal. It is skipped without that variable.
