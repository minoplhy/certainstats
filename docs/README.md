# CertainStats documentation

CertainStats is a self-hosted server monitoring platform: agents report system metrics, collectors report what software like CrowdSec and Knot DNS sees, and you view it all in a private admin panel and public status pages.

The docs are split by who they are for:
- **[Operate](#operate)**: running a CertainStats server.
- **[Develop](#develop)**: changing the code.

## I want to…

| Task | Page |
| --- | --- |
| Install CertainStats or upgrade it | [Install and upgrade](operate/install-and-upgrade.md) |
| Look up an environment variable | [Configuration](operate/configuration.md) |
| Put it behind a reverse proxy | [Configuration: where the panel lives](operate/configuration.md#where-the-panel-and-dashboards-live), [Security: hosts and proxies](operate/security.md#hosts-and-reverse-proxies) |
| Know what to back up | [Install and upgrade: back up](operate/install-and-upgrade.md#back-up) |
| Script against the panel API | [Security: CSRF](operate/security.md#scripts-and-api-clients-csrf) |
| Understand rate limits, 429 and 503 | [Security: limits](operate/security.md#limits) |
| Read the maintenance log, or chase growing memory | [Troubleshooting](operate/troubleshooting.md) |
| Share a public status page | [Status pages](operate/dashboards.md) |
| Get notified when something breaks | [Alerts](operate/alerts.md) |
| Change my password or sign out other devices | [Security: your account](operate/security.md#your-account) |
| Add, reinstall or remove an agent | [Agents](operate/agents.md) |
| Monitor CrowdSec or Knot DNS | [Collectors](operate/collectors/README.md), then [CrowdSec](operate/collectors/crowdsec.md) or [Knot DNS](operate/collectors/knot.md) |
| Check that hosts and services answer | [Network monitoring](operate/network-monitoring.md) |
| Understand how the server works | [Architecture](develop/architecture.md) |
| Run the tests | [Testing](develop/testing.md) |
| Add an agent driver | [Agent internals: adding a driver](develop/agents.md#adding-a-driver) |
| Add a collector type | [Collector internals: adding a type](develop/collectors/README.md#adding-a-type) |

## Operate

For people running a CertainStats server.

| Page | Covers |
| --- | --- |
| [Install and upgrade](operate/install-and-upgrade.md) | Installing with Docker Compose, what is stored, backups, upgrades and rollbacks, notes for upgrading from older releases |
| [Configuration](operate/configuration.md) | Every server environment variable: URL/path mode, proxies, rate limits, retention, diagnostics |
| [Security](operate/security.md) | Hosts and proxies, sign-in, your account, CSRF for scripts, public dashboards, browser hardening, webhooks, all limits |
| [Troubleshooting](operate/troubleshooting.md) | The hourly maintenance log, memory log, profiling, memory and disk growth, offline agents |
| [Agents](operate/agents.md) | Supported agents, adding and managing them, the Agent Hub, how their data behaves |
| [Status pages](operate/dashboards.md) | Creating public status pages, what visitors see, privacy |
| [Alerts](operate/alerts.md) | Alert rules and conditions, notification targets, message templates, incident history |
| [Collectors](operate/collectors/README.md) | What collectors are, setting one up, collector settings |
| [CrowdSec](operate/collectors/crowdsec.md) | Collecting CrowdSec alerts, machine and bouncer health, alert rules, retention, the page |
| [Knot DNS](operate/collectors/knot.md) | Collecting Knot DNS counters, zones and dnstap, checks, alert rules, the page |
| [Network monitoring](operate/network-monitoring.md) | Network monitors on agents, the Network page, loss alerts |

## Develop

For people changing the code.

| Page | Covers |
| --- | --- |
| [Architecture](develop/architecture.md) | Parts, data flow, ingestion and recovery, metrics, alerts, sessions, caching, code layout, tables |
| [Coding standards](develop/coding-standards.md) | Go and frontend coding rules |
| [Caching standards](develop/caching-standards.md) | How to cache responses, coalesce builds and invalidate; the static asset pipeline |
| [Testing](develop/testing.md) | Running Go and browser tests, leak and soak tests, release checks |
| [Frontend](develop/frontend.md) | Design rules, tokens, components, JavaScript modules, templates, routes |
| [Frontend JavaScript build](develop/frontend-js-build.md) | Asset areas, bundle sources, build rules, CI checks |
| [Browser protocol](develop/browser-protocol.md) | The live feed in JSON and Protobuf, schema rules, regeneration |
| [Agent internals](develop/agents.md) | Provisioning, Beszel/LTstats/HetrixTools wire formats, parsing, normalisation, ingestion, API, adding a driver |
| [Collector internals](develop/collectors/README.md) | Collector protocol, API, storage, adding a type |
| [CrowdSec internals](develop/collectors/crowdsec.md) | Local API reads, health reads, messages, storage, check registry, API |
| [Knot DNS internals](develop/collectors/knot.md) | Control socket, messages, storage, API |
| [Network monitoring internals](develop/network-monitoring.md) | Capabilities, sync, ingestion, API, live feed, caching |

## Case studies

| Page | Covers |
| --- | --- |
| [TSDB block growth](case-study/tsdb-block-growth.md) | How unmerged TSDB blocks made memory climb from 50 MB to 280 MB, and the fix |

## Archive

Historical documents that no longer describe the code: see [archive](archive/README.md).
