# Collectors

A **collector** is a small program, `certainstats-collector`, that you install on a host to send CertainStats data from software running there. This page explains how collectors work and how to set one up. It is for people running a server. Developers: see [collector internals](../../develop/collectors/README.md).

## Collectors and agents

- An **agent** reports a machine's own metrics: CPU, memory, disk, network. See [agents](../agents.md).
- A **collector** reports what other software on that machine sees. What it sends is called a **type**. The types are [CrowdSec](crowdsec.md) and [Knot DNS](knot.md). One collector can send several types.

A collector can be linked to the node it runs on, but it never appears in the Agent Hub, on public dashboards or in node counts. Collectors are private: only their owner sees them.

## Where to find them

- The **Collectors** page (`/collectors`, in the main navigation) lists your collectors.
- Its **Types** section lists every type, with how many collectors report it. Each type has its own page at `/collectors/<type>`, such as `/collectors/crowdsec`. The older addresses `/<type>` redirect there. Type pages are not in the main navigation.

## How it connects

- The collector dials out to the panel and keeps one WebSocket open, so the host needs no open port.
- It keeps no state of its own. Anything it needs to resume, such as a read position, lives on the server and is sent when it connects. A restart or an outage only delays data, and reinstalling loses nothing.
- If the connection drops, it reconnects with a randomised backoff of up to five minutes.
- Each collector has one live connection; a new one replaces the old one.
- The connection is closed, and the collector reconnects to pick up the change, when you:
  - reset its token;
  - remove the collector;
  - enable or disable one of its types.
- A wrong, reset or removed token is refused before the connection opens.
- The server drops a connection that sends nothing for 90 seconds. Collectors send a keepalive every 30 seconds.

Connection limits (256 live collectors, 30 attempts per minute per IP) are listed in [security](../security.md#connections).

## Setting one up

### 1. Create it in the panel

1. On the Collectors page, choose **Add collector**.
2. Give it a name, optionally the node it runs on, and tick the types it collects.
3. The install dialog opens with the Hub URL, the token, and setup steps for systemd and Docker Compose.

The Hub URL in the steps is `PANEL_URL` when that is set; otherwise it is built from the request.

Later changes:
- **Install** on the collector's row opens the install dialog again at any time.
- Resetting the token opens the dialog with the new token.
- **Collect** on the row adds another type and shows the updated install steps. The host must then switch the new type on.
- **Disable** switches a type off again.
- `/collectors?add=crowdsec` opens the add form with CrowdSec already ticked.

### 2. Check the network path

- The panel URL must be reachable from the host.
- A reverse proxy in front of the panel must pass WebSocket upgrades and keep idle connections open longer than 30 seconds. Beszel agents need the same.

### 3. Install the collector

Use either:
- the `certainstats-collector-linux-<arch>` release files, or
- the image `ghcr.io/minoplhy/certainstats-collector`. Its entrypoint is the collector, so flags pass straight through, such as `docker compose run --rm certainstats-collector --check`.

### 4. Configure it

<a id="collector-settings"></a>The collector reads these environment variables:

| Variable | Meaning |
| --- | --- |
| `HUB_URL` | The panel URL, such as `https://stats.example.com`. |
| `TOKEN` | The token from step 1. |
| `COLLECT_<TYPE>` | Switches a type on: `COLLECT_CROWDSEC=true`, `COLLECT_KNOT=true`. With none set, the collector exits and names every switch. |

Each type adds its own settings, prefixed with its name. Some switch on optional reading that is off by default, such as CrowdSec's `CROWDSEC_HEALTH` or Knot's `KNOT_DNSTAP`. See [CrowdSec](crowdsec.md#setup) and [Knot DNS](knot.md#setup).

### 5. Keep the clock right

Run NTP on the host. Data and read positions are timestamps.

### Command-line flags

| Flag | What it does |
| --- | --- |
| `--check` | Tests each prerequisite and names the first one that fails. |
| `--once` | Runs one collection pass and exits. |
| `--version` | Prints the version. |

## Status

The Collectors page has one row per collector. For each type, it shows one of:

- "Waiting for first connection"
- "Offline"
- "Connected, CrowdSec not switched on (COLLECT_CROWDSEC=true)". Each type names its own switch.
- "Connected, <error>"
- "Reporting"

## Tokens

The token is stored, as agent tokens are, so the install steps can be shown again. Connections are matched by its hash. Collectors created before tokens were stored show a placeholder and a note to reset the token.

Removing a collector keeps a record of it, with a token that can never match, so data it already sent keeps its name.

## Storage

Time series from collectors go to their own TSDB, `DATA_DIR/collector_tsdb`, never to the agent TSDB. A busy collector can therefore neither push out agent metrics nor take agent query slots.

- Size is set by `COLLECTOR_RETENTION_DURATION` (default 90 days) and `COLLECTOR_STORAGE_MAX_BYTES`, independent of the agent TSDB settings. See [configuration](../configuration.md#retention-and-storage).
- At most 8 reads run at once. A read that waits 2 seconds for a slot gets 503.

Each type keeps its other data where it suits it, such as `crowdsec.db` or `knot.db`.
