# Testing

This page lists how to run the test suites and what to check before a release. It is for developers. Feature-specific tests are listed on each internals page.

## Go

```sh
go test ./...                       # everything; this is what CI runs (with -v)
go test -tags embed ./...           # with the web assets embedded, as in release builds
go test -race ./internal/agent ./internal/networkservice ./internal/store/sqlite ./internal/ws ./internal/routine
```

- The race checks cover the agent, network service, SQLite and WebSocket packages.
- On a machine with little memory, run one package at a time with `GOFLAGS=-p=1`.

### Leaks and memory

- **Churn tests** (`leak_test.go` in several packages) create and remove agents, viewers and cache entries many times and check that nothing is left behind.
- **goleak** checks run in the normal test run in the `ws`, `routine`, `networkservice`, `collector` and `agent` packages (`main_test.go`). A test that leaves a goroutine running fails.
- **Soak test** (opt-in): `go test -tags soak -run Soak -v ./internal/agent/`. `SOAK_CYCLES` sets the number of cycles (default 1000). CI runs it only on manual dispatch, with a 20-minute timeout.

### Real Beszel agent

Set `BESZEL_TEST_AGENT_BINARY` to a Beszel agent binary and run:

```sh
go test ./internal/agent -run '^TestRealBeszelNetworkMonitoring$' -v
```

It is skipped without that variable. See [network monitoring internals](network-monitoring.md#tests).

## Browser JavaScript

```sh
npm ci --prefix web --no-audit --no-fund
node --test web/tests/*.test.cjs
```

CI also checks that generated files are up to date:

```sh
npm ci --prefix tools/browser-protocol --no-audit --no-fund
npm ci --prefix tools/js-bundle --no-audit --no-fund
node tools/browser-protocol/generate.cjs --check
node tools/js-bundle/build.cjs --check
```

See [frontend JavaScript build](frontend-js-build.md) and [browser protocol](browser-protocol.md).

## The archived React frontends

`frontend-admin/` and `frontend-public/` are archives. Release CI does not build them, and the normal `embed` build tag does not include them. Building with `-tags 'archive embed'` explicitly opts into the old frontend packages.

## Before a release

1. Run the Go tests, the race checks, the browser JavaScript tests and an embedded build.
2. Check pages in a browser at widths from 320 to 1440 px, in both light and dark themes.
3. Before changing the default limits, run representative load tests against production-like storage and agent counts. The synthetic quota and cache tests check behaviour, not production capacity.
4. Release separate kinds of change (security and privacy, metric correctness, UX, deployment) as separate reviewed stages.

Operators' side of a release (backups, upgrade notes) is in [install and upgrade](../operate/install-and-upgrade.md).
