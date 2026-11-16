# CrowdSec

CrowdSec is a [collector](README.md) type. A collector next to CrowdSec reads its alerts and sends them to CertainStats, where the CrowdSec page lets you explore them. This page covers setup, the optional machine and bouncer health checks, alert rules, retention and the page. It is for people running a server. Developers: see [CrowdSec internals](../../develop/collectors/crowdsec.md).

- Alerts are private to their owner; nothing reaches public dashboards.
- No change to CrowdSec's profiles or notification plugins is needed.

**Terms used here.** CrowdSec's **Local API** (LAPI) is the central service its components talk to. A **machine** is a CrowdSec log processor (CrowdSec calls it an agent; this page says "CrowdSec agent" to keep it apart from CertainStats agents). A **bouncer** is a component that applies decisions, such as a firewall bouncer that blocks banned IPs. **CAPI** is CrowdSec's central API, which supplies community blocklists.

## What the collector reads

- The collector signs in to the Local API as a machine and lists alerts with every decision attached. It only reads.
- A machine login could also write alerts and delete decisions, since CrowdSec has no read-only machine. The setup steps therefore register a machine used only by the collector, which can be removed on its own.
- **Raw log lines never leave the host.** Each alert's `events` and `meta`, which carry log lines, are dropped before anything is sent.
- **Community blocklists are left out**, as `cscli alerts list` does. Each blocklist pull arrives as one alert holding thousands of decisions, and none of them is an attack seen on this host.
- An alert with more than 500 decisions is sent with the first 500.
- Nothing is skipped: each pass re-reads the last hour, and alerts already stored are ignored.

### Waiting for CrowdSec to start

CrowdSec can take a while to start, and Compose's `depends_on` only waits for its container, not for its Local API.

- For the first 2 minutes after the collector starts, until the Local API first answers, an unreachable Local API is logged as `waiting for the Local API` and not reported to CertainStats.
- The collector retries after 5 seconds, then doubles the wait up to the poll interval.
- After those 2 minutes, or once the Local API has answered, an outage is reported at once and retried the same way.
- `--once` always reports at once.
- A Compose healthcheck with `depends_on: condition: service_healthy` shortens the wait further.

While the Local API is unreachable, the collector stays connected and reports the error. The Collectors page and the CrowdSec page headline show it.

## Setup

On top of the [steps every collector needs](README.md#setting-one-up):

1. Tick CrowdSec when creating the collector, and set `COLLECT_CROWDSEC=true` on the host.
2. Install the collector on the machine that runs the Local API. In a multi-server setup, that is the central server only. Use one collector per Local API.
3. Register its machine login, as root:

   ```sh
   mkdir -p /etc/certainstats
   cscli machines add certainstats --auto -f /etc/certainstats/crowdsec-lapi.yaml
   chmod 600 /etc/certainstats/crowdsec-lapi.yaml
   ```

   The file holds `url`, `login` and `password`. `CROWDSEC_LAPI_CREDENTIALS` points to it and defaults to that path. `--check` warns when the file is readable by other users.
4. If CrowdSec runs in Docker:
   - the collector must share CrowdSec's network;
   - the Local API must listen on that network, not only on 127.0.0.1;
   - `url` in the file must name the container (`http://crowdsec:8080`);
   - the file must be mounted into the collector.

Not needed: an open port, a bouncer key, or access to CrowdSec's log files. Access to CrowdSec's database is not needed either, unless you switch on [machine and bouncer health](#machine-and-bouncer-health).

| Variable | Default | Meaning |
| --- | --- | --- |
| `COLLECT_CROWDSEC` | off | `true` switches the type on |
| `CROWDSEC_LAPI_CREDENTIALS` | `/etc/certainstats/crowdsec-lapi.yaml` | The machine login file from step 3 |
| `CROWDSEC_HEALTH`, `CROWDSEC_DB`, `CROWDSEC_HEALTH_READ` | off | Machine and bouncer health, below |

## Machine and bouncer health

A Local API can serve many machines and bouncers. This optional part shows whether each one is alive, hints when a bouncer may not be applying decisions, and can notify you when one goes quiet and when it comes back.

### Read-only, and only when the host says so

The Local API has no route that lists machines or bouncers; `cscli` reads CrowdSec's database for that, and so does the collector. It never changes anything in CrowdSec:

- **Off by default.** The collector opens CrowdSec's database only when the host sets `CROWDSEC_HEALTH=true`. Without it, no database code runs, no path is probed, and the panel cannot switch it on.
- **Its own fixed queries.** It reads with its own `SELECT` statements. It does not use CrowdSec's Go client, which changes the database file's permissions and runs a schema migration every time it opens.
- **Read-only connections.** SQLite is opened read-only, so a missing file is an error, never a new file. PostgreSQL and MySQL queries run in read-only transactions.
- **No CrowdSec config.** CrowdSec's `config.yaml` is never read, because it holds read-write credentials.

| Variable | Default | Meaning |
| --- | --- | --- |
| `CROWDSEC_HEALTH` | off | `true` lets the collector read CrowdSec's database |
| `CROWDSEC_DB` | `/var/lib/crowdsec/data/crowdsec.db` | A SQLite path, or `postgres://user:password@host:5432/crowdsec` or `mysql://user:password@host:3306/crowdsec` |
| `CROWDSEC_HEALTH_READ` | all four | Which parts may be read: `machines`, `bouncers`, `metrics`, `decisions`, separated by commas |

- **Docker:** mount CrowdSec's data directory into the collector read-only (`crowdsec-db:/var/lib/crowdsec/data:ro`).
- **PostgreSQL or MySQL:** give the collector its own user with `SELECT` on `machines`, `bouncers`, `metrics` and `decisions` only. `--check` warns when a PostgreSQL user could write.
- `--check` reports health on a line of its own and never fails because of it.

### What is read

| Part | What | Used for | How often |
| --- | --- | --- | --- |
| `machines` | Each machine's last heartbeat, version, address, validation and OS | Is the CrowdSec agent on that machine running | Every minute |
| `bouncers` | Each bouncer's last pull, type, version, address and whether it is revoked | Is the bouncer fetching decisions | Every minute |
| `metrics` (CrowdSec 1.6.3+) | Each bouncer's newest usage report, reduced on the host to three numbers: decisions held per origin, packets dropped, packets processed | What the bouncer says it holds and drops | Every 5 minutes |
| `decisions` | A count of active `ban` decisions on IPs and ranges, by origin | The other side of that comparison | Every 5 minutes |

- A part the host leaves out is never queried. The checks that need it are listed on the page as not available.
- A part that does not exist in your CrowdSec version (such as `metrics` before 1.6.3) is simply skipped.
- The bouncer's usage report itself never leaves the host.

### What each signal can prove

Everything here is the component's own word, recorded by the Local API:

- **A fresh heartbeat** shows the CrowdSec agent runs and reaches the Local API.
- **A fresh pull** shows the bouncer runs and fetches decisions. It does not show the decisions reached the firewall. A bouncer in live mode only asks when it has traffic, so mute it or allow a longer quiet time.
- **Decision counts are a hint, not proof.** A bouncer never reports that applying a decision failed; it only logs that on its own host. But the firewall bouncer's usage reports, sent every 15 minutes, are read from the kernel: decisions held per origin is the real nftables set or ipset entry count (0 when `ipset list` fails), and dropped and processed are rule counters. So a firewall holding far fewer decisions than the Local API has active suggests they are not being applied. The reason is only in the bouncer's log.

### Checks

Health is a list of named checks. On the CrowdSec page, a check that fails at its default numbers is a **caution** (amber). A caution opens no incident, sends nothing and never changes the headline. To be notified, create an [alert rule](#alert-rules).

| Check | Applies to | Fails when (defaults) | What it can show |
| --- | --- | --- | --- |
| `heartbeat` | machines | Quiet for 5 minutes | The CrowdSec agent runs and reaches the Local API |
| `pull` | bouncers | Quiet for 60 minutes | The bouncer runs and fetches decisions |
| `held` | bouncers | Holds under 50% of at least 20 active decisions of an origin it reports, on 2 usage reports in a row | It may not be applying decisions |
| `siblings` | bouncers sharing a key | Holds under 10% of what its largest sibling holds, that sibling holding at least 1,000, on 2 usage reports in a row | Its ipset or nftables set may be failing |

More detail on `held` and `siblings`:
- **`held`:** a usage report with no decision counts at all counts as holding 0 of every active origin, once the bouncer is known to hold decisions. It is known to when it has sent decision counts before, or when it counts packets, which only a firewall bouncer does. A bouncer in live mode does neither and is not judged.
- **`siblings`:** a usage report with no decision counts at all (what a failing ipset sends) counts as holding 0.

**Shared keys.** When one bouncer key is used from several addresses, CrowdSec creates one entry per address, named `name@ip`. Those entries apply the same decisions, so `siblings` compares each with the others of its key. One holding 0 while its siblings hold 30,000 is what a firewall bouncer looks like when `ipset` fails. The base name is found by stripping trailing `@<ip>` parts, as CrowdSec does.

**When components are judged.** A component is judged only while its collector is connected and its last report is error-free and younger than five minutes. A collector that died, or a host that switched health off, therefore never sets off a round of notifications. Never judged:
- muted components;
- revoked bouncers;
- machines that are not validated;
- the collector's own machine login, which never sends a heartbeat.

### Alert rules

Notifications come from ordinary [alert rules](../alerts.md) on the Alerts page. **New rule** offers a CrowdSec group under **When**, with one condition per check: `crowdsec_heartbeat`, `crowdsec_pull`, `crowdsec_held`, `crowdsec_siblings`.

In place of **Servers**, a rule has a **Watch** list per collector, showing machines or bouncers depending on the condition. It can watch:
- **every machine and bouncer** of a collector, including ones added later;
- one machine;
- one shared bouncer key: every address using it, new addresses included;
- one bouncer entry.

Timing:
- The rule's threshold sets the check's main number (minutes, or a percentage). The other numbers keep their defaults.
- The check does its own timing, so there is no operator or duration.
- A check that needs several reports in a row counts the reports seen since the rule was created. Bouncers send usage reports every 15 minutes, so **a new `held` or `siblings` rule can take up to half an hour to fire.**

Incidents:
- A firing rule is an incident in the Alerts incident history like any other. Delivery, retries and message templates are the same. `{{NICKNAME}}` is the machine or bouncer; `{{COLLECTOR}}`, `{{SUBJECT_KIND}}` and `{{DETAIL}}` are also available.
- On the CrowdSec page, the component turns red and names the rule.
- An incident closes without a notification when its rule is disabled, deleted or no longer covers the component, or when the component is muted, deleted from CrowdSec, or its collector is removed.
- A collector that cannot be judged leaves its incidents as they are.

## Retention

Alerts live in their own file, `DATA_DIR/crowdsec.db`. Include it in [backups](../install-and-upgrade.md#back-up).

| Variable | Default | Meaning |
| --- | --- | --- |
| `CROWDSEC_RAW_RETENTION` | `0` (unlimited) | How long alerts and their decisions are kept, in Go duration syntax such as `4320h` |
| `CROWDSEC_ROLLUP_RETENTION` | `0` (unlimited) | How long hourly counts are kept |

- Values below one hour are rejected at startup.
- Pruning runs with the hourly maintenance in batches of 5,000 rows, removes rows of deleted accounts, and logs the table sizes.
- **Suggested setup:** keep raw alerts for 180 days and hourly counts forever. Charts then reach back years while disk use stays bounded.

### Detail and hourly counts

- A filter on IP, range, ASN or collector, or a search, needs raw alerts.
- Without those, ranges longer than seven days, or older than the retained raw alerts, are answered from the hourly counts.
- When you ask for detail on a period whose raw alerts were already deleted, the page says so rather than showing an empty result.
- Unique IPs cannot be counted from hourly data.

## The page

`/collectors/crowdsec`, reached from the Collectors page (`/crowdsec` redirects there).

**Top of the page.**
- A sentence headline and figures: alerts, unique IPs, active bans now and collectors reporting. **Collectors** links to the Collectors page.
- A one-row filter bar:
  - search;
  - **+ Filter**, the shared filter menu (field, is / is not, value), as on the Knot DNS page;
  - one range dropdown with the quick ranges, a custom range (up to 20 years) and **All**, a range starting at the oldest data.
- Active filters show as removable chips under the bar.

**Machines and bouncers** (once some host has switched health on).
- The top section shows how many machines and bouncers there are and how many need a look. **Machines and bouncers** opens them in a popup with two lists, laid out like the Management page.
- A machine's row opens to its address, version, system, validation and last heartbeat.
- A bouncer's row opens to everything it reported: type, version, address, last pull, when it last sent usage reports, the decisions it holds per origin next to the Local API's active count, and packets dropped and processed.
- A shared key is one row, with its entries side by side.
- Each has **Mute**.
- What needs a look comes first. Rows update in place every 30 seconds, and an open row stays open.
- A firing rule takes over the headline; a caution never does.

**Below the filters.**
- The timeline of alerts and the decisions they issued, as two series. Brush (drag across) to zoom.
- Four breakdowns, whose field can each be changed. Clicking a value filters by it; its second button excludes it.
- The alert table. Cells have copy and filter buttons, columns sort, and a row expands to show every decision.
- Clicking an IP opens its own view.

The range, filters, sort, page, breakdown fields and open IP all live in the URL, so any view can be linked and Back works.
