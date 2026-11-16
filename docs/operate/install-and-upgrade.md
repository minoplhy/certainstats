# Install and upgrade

This page covers running the CertainStats server, what to back up, and how to upgrade and roll back safely. It is for people running a server.

## Install

The server ships as a container image (`ghcr.io/minoplhy/certainstats`) and runs with Docker Compose. `docker/docker-compose.yaml` is a ready-made starting point.

1. Copy `docker/docker-compose.yaml`, and set your values in a `.env` file next to it. At minimum, choose [URL mode or path mode](configuration.md#where-the-panel-and-dashboards-live).
2. Mount a volume for the data directory (`DATA_DIR`, `/app/data` in the image).
3. Start it with `docker compose up -d`.
4. Open the setup link printed in the server log on first start (`/first-time-setup?token=…`) and create the first account. See [security](security.md#signing-in-and-first-time-setup).

About the container:
- The image is based on `alpine:latest` and runs as Alpine's default root user. No fixed UID/GID or volume ownership change is needed.
- Compose restarts it with `restart: unless-stopped`. There is no HTTP health check and no health route.
- On shutdown the server stops HTTP traffic, cancels background work, closes browser and agent connections, waits for the background maintenance routine, and then closes storage.

Collectors have their own image, built from `Dockerfile.collector`. See [collectors](collectors/README.md).

## What is stored

Everything lives in the data directory (`DATA_DIR`):

| Path | What it holds |
| :--- | :--- |
| `agent_state.db` (plus `-wal`, `-shm`) | Main SQLite database: accounts, agents, dashboards, alerts, collectors, the ingestion journal |
| `tsdb/` | Agent metrics (Prometheus TSDB) |
| `collector_tsdb/` | Collector metrics, such as Knot DNS counters |
| `crowdsec.db` | CrowdSec alerts, decisions and health |
| `knot.db` | Knot DNS dnstap buckets and zones |

Retention defaults differ per store; see [configuration](configuration.md#retention-and-storage).

## Back up

Back up the **whole data directory**: both TSDB folders and every `.db` file with its `-wal` file. For a consistent copy, stop the server (or at least stop ingestion) first.

Always back up before an upgrade that changes storage, and keep the backup until you have checked, after the upgrade, that:
- database migrations ran;
- recovery and replay of pending data worked;
- dashboard visibility changes and revocations still apply;
- sign-in and sessions behave;
- storage growth looks normal.

Test recovery and migration on a copy of the data directory when you can.

## Upgrade

1. Read the notes below for anything that applies to your upgrade.
2. Back up the data directory.
3. Pull the new image and restart: `docker compose pull && docker compose up -d`.

SQLite migrations only add things and are safe to run more than once. If a migration hits an unexpected error, the server refuses to start rather than run on a half-migrated database.

### Rolling back

Stop the server, restore the data directory from your backup, and start the previous image. Rolling back without a restore is also safe for the TSDB block-merge change described below.

### Upgrading from older releases

**TSDB block merging.** Earlier releases never merged TSDB blocks, so a new 2-hour block stayed open every 2 hours for good. Open files, mapped memory and the cost of long-range charts grew without limit. Both TSDBs now merge blocks (sizes in [configuration](configuration.md#retention-and-storage)). On the first start after upgrading:
- Existing blocks merge in the background over minutes to hours, with high CPU and disk activity.
- Merging needs temporary extra disk space, up to the size of the TSDB being merged.
- Ingestion and the UI keep working meanwhile.
- Merging is lossless: chart values are unchanged, long-range charts get faster, and short ranges behave as before.
- Rolling back is safe: older releases read merged blocks and simply stop merging new ones.
- Back up `tsdb` and `collector_tsdb` first.
- Track progress with `ls -d $DATA_DIR/tsdb/01*/ | grep -v tmp | wc -l`. The plain `01*` glob also counts merge work folders. With `MEMORY_LOG=true`, the hourly memory line shows `tsdb_blocks` and `collector_tsdb_blocks`.

The [block growth case study](../case-study/tsdb-block-growth.md) has the production measurements behind this change.

**Ingestion journal retention.** Completed journal entries are now deleted after `INGESTION_JOURNAL_RETENTION` (default `24h`). The first hourly cleanup after upgrading may delete many rows. It then switches the main database to incremental vacuum with one full `VACUUM`, logged as `converting database to incremental auto_vacuum (one-time VACUUM)`.

**First-time setup.** The `/api/first-time-setup/restart` route has been removed. Setup uses the link printed at startup.

**Health checks.** HTTP health checks have been removed from the compose files, and no health route was added.

**Byte series.** Stored byte series are unchanged. History responses gained rate fields, and older history without sample durations is estimated and marked as such, without rewriting stored data.

## Monitoring the server itself

- Watch the process and the volume, for restarts and for storage growth.
- The server logs ingestion failures and restarts.
- The hourly maintenance log reports cache, rate-limit and journal figures. See [troubleshooting](troubleshooting.md) for what they mean.
