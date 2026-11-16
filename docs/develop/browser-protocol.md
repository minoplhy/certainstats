# Browser WebSocket protocol

This page describes the live feed that the admin panel and public dashboards receive over WebSocket, in its two formats: JSON (the default) and experimental Protobuf. It is for developers. Operators: the `WS_PROTOBUF` setting is described in [configuration](../operate/configuration.md#browser-live-feeds).

## Pulses

- The server sends a full pulse every ten seconds. There are no delta updates and no browser commands.
- Empty pulses also count as fresh: they tell the browser the feed is alive.
- A browser marks the feed stale after 45 seconds without a pulse.
- The agent map uses private IDs for signed-in owners and public IDs for dashboard viewers.
- Owner and public permissions are applied before encoding. Nested disk fields are filtered one by one.
- The usual protections apply in both formats: authentication, origin checks, revocation, connection limits, bounded mailboxes and write deadlines.

## JSON (default)

Text frames of the form `{"type":"agent_update","data":{...}}`.

- Owner fields use snake_case. Public metric fields use older PascalCase names, with snake_case nested disk fields and status fields.
- Denied fields are omitted. Unavailable readings are `null`. A measured zero or false is sent explicitly.
- JSON connections need no subprotocol, and reject any subprotocol offer, including Protobuf.

## Protobuf (experimental)

Enabled with `WS_PROTOBUF=true` on the server, for both dashboards.

### Handshake

- Connections must offer the `certainstats.protobuf.v1` subprotocol. A missing or unsupported offer gets HTTP 426.
- The page exposes its mode as inert HTML configuration, and loads the generated decoder only in Protobuf mode.
- There is no automatic negotiation or fallback between formats.
- HTTP JSON APIs and agent ingestion protocols are not affected.
- Tabs opened before a mode change must reload. After three consecutive failures to connect, the page offers a reload button.

### Schema

The schema is `protocol/browser/v1/telemetry.proto`. Each binary frame holds a `TelemetryEnvelope` with one full `TelemetryPulse`.

How presence is encoded:

| On the wire | Means |
| --- | --- |
| Numeric wrapper absent | Not supplied, or not allowed for this viewer |
| Numeric wrapper present but empty | Reading unavailable |
| Scalar value present | A measured value, including zero |
| Optional boolean absent | Status unknown (distinct from false) |
| Collection absent vs present and empty | Not supplied vs explicitly empty |

Units:
- Rates are bytes per second, and byte counts are `uint64`.
- Timestamps keep seconds and nanoseconds.
- Decoding keeps `uint64` precision with Long objects. The UI adapter converts them to JavaScript numbers for the renderers, which lose precision above `Number.MAX_SAFE_INTEGER`, as before.

### Changing the schema

- Keep changes within v1 additive and compatible.
- Never reuse field numbers or names. When retiring a field, reserve both.
- A breaking change needs a new subprotocol.
- Deploy the backend and browser assets together.

## Regenerating

The generated Go code and the bundled browser decoder are checked in. Normal Go builds, embedded builds and Docker builds need neither Node nor protoc. The browser script is served locally with fingerprinting and integrity checks: no CDN, no inline executable code, no schema loaded at runtime.

To regenerate, install protoc **36.1**, the Go version in `go.mod`, and Node 22. The generator builds protoc-gen-go from the module's pinned Protobuf dependency. JavaScript tooling versions and their dependencies are pinned in the tool's own package manifest and lockfile.

```sh
npm ci --prefix tools/browser-protocol --no-audit --no-fund
node tools/browser-protocol/generate.cjs
node tools/browser-protocol/generate.cjs --check
```

Change the schema or `tools/browser-protocol/browser.cjs`, then regenerate. Never edit the generated decoder or Go file directly. CI checks that the generated files are up to date. This tooling is separate from the archived React projects.

### Fixtures

Shared fixtures are encoded by Go and decoded by the actual shipped browser bundle. After an intentional protocol change, update them and rerun the checks:

```sh
UPDATE_PROTOCOL_FIXTURES=1 go test ./internal/ws -run TestBrowserProtocolFixtures
npm ci --prefix web --no-audit --no-fund
node --test web/tests/*.test.cjs
go test ./...
go test -race ./internal/ws ./internal/routine
go test -tags embed ./...
go build -tags embed -o /tmp/certainstats ./cmd/certainstats
```

`go test -v ./internal/ws -run TestBrowserProtocolFixtures` reports fixture payload sizes. These compare sample uncompressed payloads, not production capacity or latency. The decoder's download size and browser decoding cost also matter when weighing Protobuf against JSON.
