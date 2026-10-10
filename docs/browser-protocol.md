# Browser WebSocket protocol

Admin and public dashboard live feeds default to JSON text frames with
`{"type":"agent_update","data":{...}}`. Owner fields use snake_case;
public metric fields use legacy PascalCase, with snake_case nested disk fields
and status fields. Denied fields are omitted, unavailable readings are null,
and measured zero and false remain explicit.

Set the process environment `WS_PROTOBUF=true` to enable experimental binary
Protocol Buffers for both dashboards. Only the exact value `true` enables it.
The mode is read once at startup. Restart the server (recreate Compose containers)
and reload open tabs when changing modes. Compose forwards `.env` settings;
the application does not load dotenv files.

Experimental connections require `certainstats.protobuf.v1`; missing or
unsupported offers receive HTTP 426. Default JSON connections need no
subprotocol and reject subprotocol offers, including Protobuf. The page exposes
its mode as inert HTML configuration and loads the generated decoder only in
experimental mode. There is no automatic negotiation or fallback. HTTP JSON
APIs and agent ingestion protocols are unchanged.

Each binary frame contains a `TelemetryEnvelope` with a full `TelemetryPulse`.
Its agent map uses private IDs for authenticated owners and public IDs for
dashboard viewers. Empty pulses establish feed freshness too. There are no
delta updates or browser commands. The existing ten-second pulse cadence and
45-second stale indication remain in effect.

The schema lives in `protocol/browser/v1/telemetry.proto`. Owner and public
permissions are applied before encoding; nested disk fields are individually
filtered. An absent numeric wrapper means a field was not supplied or allowed;
a present empty wrapper means an unavailable reading; a present scalar value
includes measured zero. Optional booleans distinguish absent status from false.
Collection messages distinguish absence from an explicitly empty collection.
Rates remain bytes per second, byte counts use uint64, and timestamps retain
seconds and nanoseconds. Decoding preserves uint64 precision with Long objects;
the UI adapter converts them to JavaScript numbers for the existing renderers,
which retain their existing precision limits above Number.MAX_SAFE_INTEGER.

Keep changes within v1 additive and compatible. Never reuse field numbers or
names; reserve both when retiring a field. Breaking changes require a new
subprotocol. Deploy the backend and browser assets together. Tabs opened before a mode change must reload. New clients offer a reload button
after three consecutive failures to open a connection. There is no format fallback. Existing authentication, origin checks, revocation, connection limits,
bounded mailboxes, and write deadlines still apply.

## Regeneration

Generated Go code and the bundled static browser decoder are checked in. Normal
Go builds, embedded builds, and Docker builds require neither Node nor protoc.
The browser script is served locally with fingerprinting and integrity checks;
no CDN, inline executable code, or runtime schema loading is involved.

For regeneration, install protoc **36.1**, Go matching `go.mod`, and Node 22.
The generator builds protoc-gen-go from the module's pinned Protobuf dependency.
JavaScript tooling versions and transitive dependencies are pinned in the
dedicated package manifest and lockfile.

```sh
npm ci --prefix tools/browser-protocol --no-audit --no-fund
node tools/browser-protocol/generate.cjs
node tools/browser-protocol/generate.cjs --check
```

Change the schema or `tools/browser-protocol/browser.cjs`, then regenerate;
never edit the generated decoder or Go file directly. CI verifies regeneration
and runs the active JavaScript tests. This tooling is separate from the archived
React projects.

Shared fixtures are encoded by Go and decoded by the actual shipped browser
bundle. To update them after an intentional protocol change:

```sh
UPDATE_PROTOCOL_FIXTURES=1 go test ./internal/ws -run TestBrowserProtocolFixtures
npm ci --prefix web --no-audit --no-fund
node --test web/tests/*.test.cjs
go test ./...
go test -race ./internal/ws ./internal/routine
go test -tags embed ./...
go build -tags embed -o /tmp/certainstats ./cmd/certainstats
```

`go test -v ./internal/ws -run TestBrowserProtocolFixtures` reports fixture
payload sizes. These measurements compare sample uncompressed payloads, not
production capacity or latency. Generated decoder download size and browser
decoding costs also matter when assessing the overall tradeoff.
