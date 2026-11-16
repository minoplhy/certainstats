# Troubleshooting

This page explains the server's hourly maintenance log, the opt-in diagnostics, and the checks to run when memory, disk or CPU grow. It is for people running a server.

## The hourly maintenance log

Once an hour the server cleans up storage and logs a few `[Maintenance]` lines. With default settings they look like this:

```
[Maintenance] response_cache=map[bytes:0 entries:0 evictions:0 hits:0 misses:0] builds=map[active:0 coalesced:0 waiting:0] public_limits=map[build_rejected:0 ips:0 overflow:0 request_rejected:0]
[Maintenance] ingestion_journal_pending=0
[Maintenance] ingestion_journal_pruned=0
[Maintenance] crowdsec_alerts=5617 crowdsec_rollup=1431 pruned_alerts=0 pruned_rollup=0
[Knot] rolled 1 dnstap hours
[Maintenance] knot_pruned_minute=0 knot_pruned_hourly=0 knot_pruned_zones=0
[Maintenance] network_samples=map[bytes:51840 entries:8 evictions:0 hits:0 misses:0 tsdb_fallbacks:0]
```

| Field | Meaning |
| :--- | :--- |
| `response_cache` | The server's cache of computed responses: bytes held, entries, evictions, hits and misses. |
| `builds` | Uncached responses being computed (`active`), requests sharing another request's build (`coalesced`), and requests waiting for capacity (`waiting`). |
| `public_limits` | Rate-limit rejections for requests and builds, IP addresses tracked, and use of the shared overflow budget. See [security](security.md#rate-limits). |
| `ingestion_journal_pending` | Agent batches received but not yet fully stored. Should be 0 or close to it; a value that keeps rising means storage writes are failing. |
| `ingestion_journal_pruned` | Completed journal entries deleted under `INGESTION_JOURNAL_RETENTION`. |
| `crowdsec_*` | CrowdSec rows kept and deleted under the CrowdSec retention settings. |
| `knot_pruned_*`, `rolled … dnstap hours` | Knot DNS minute buckets rolled into hours, and buckets and zones deleted under retention. |
| `network_samples` | The in-memory cache of recent network monitor readings; `tsdb_fallbacks` counts reads that had to go to the TSDB instead. |

The server also logs ingestion failures and restarts.

## Memory log (`MEMORY_LOG=true`)

Off by default. With `MEMORY_LOG=true`, the hourly log gains one more line:

```
[Maintenance] memory heap_inuse=67.2MiB heap_alloc=62.9MiB sys=147.9MiB next_gc=91.4MiB num_gc=72 goroutines=237 realtime_agents=16 collector_tsdb_blocks=12 io_states=16 tsdb_blocks=16
```

| Field | Meaning |
| :--- | :--- |
| `heap_inuse`, `heap_alloc` | Memory the Go program is actually using. Flat over hours means no leak in Go code. |
| `sys` | Memory the Go runtime has ever reserved. It never shrinks, even after memory is given back to the operating system, so it does not track resident memory. |
| `next_gc`, `num_gc` | The heap size that triggers the next garbage collection, and how many collections have run. |
| `goroutines` | Concurrent tasks. Should stay roughly level. |
| `realtime_agents` | Agents in the live readings cache. |
| `io_states` | Agents with stored network/disk counter state. Should match the number of agents. |
| `tsdb_blocks`, `collector_tsdb_blocks` | Block counts of the two TSDBs. Tens is normal; a count that keeps rising means blocks are not merging. |

## Profiling (`PPROF_ADDR`)

Off by default. `PPROF_ADDR=127.0.0.1:6060` opens a Go profiling port, on loopback only and separate from the app.

- Take a heap profile with `go tool pprof http://127.0.0.1:6060/debug/pprof/heap`.
- To see what is growing, take two profiles a few hours apart and compare them.
- In a container, run the command inside it, or publish the port to the host's loopback only.

## When memory keeps growing

Find out which kind of memory grows before looking for a leak. Get the process ID (in a container: `docker top <container>`), then run:

```sh
grep -E 'VmRSS|RssAnon|RssFile' /proc/$PID/status
ls /proc/$PID/fd | wc -l                                    # open files
ls -d $DATA_DIR/tsdb/01*/ | grep -v tmp | wc -l             # agent TSDB blocks
ls -d $DATA_DIR/collector_tsdb/01*/ | grep -v tmp | wc -l   # collector TSDB blocks
```

- **`RssAnon` grows:** the program's own memory is growing. Turn on `MEMORY_LOG` and compare `heap_inuse` and `goroutines` over a few hours. If they rise, take heap profiles.
- **`RssFile` grows, with the open-file count:** the TSDB is holding more and more block files. Check the block counts. Tens of blocks is normal; thousands means merging is not happening.
- **Counting blocks during a merge:** shortly after a merge, `01*` also matches work folders ending in `.tmp-for-creation` or `.tmp-for-deletion`. Use the `grep -v tmp` form above.

On a healthy server, block counts stay roughly level over days, and `RssFile` and the open-file count stay flat instead of growing with uptime.

The [TSDB block growth case study](../case-study/tsdb-block-growth.md) walks through a real case end to end.

## Agents look offline or charts have gaps

See also [agents: how data behaves](agents.md#how-data-behaves).

- An agent shows as **offline** after 3 minutes without data.
- A browser's live feed is marked **stale** after 45 seconds without an update.
- **Gaps** in collection are left empty, never filled with zeros. A reading the agent could not take is shown as unavailable, not as 0.
- **Lifetime totals** count traffic observed since the agent was created. Gaps are not estimated, and recreating an agent resets its totals.
