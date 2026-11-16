# Case study: memory growth from unmerged TSDB blocks

A production server's memory climbed from about 50 MB at start to about 280 MB, rising in steps that lined up with CPU spikes near 100%. The cause was the time-series database: it never merged its storage blocks, so the number of block files grew without limit. The fix shipped in commit `6a83129` and halved the server's memory.

## Symptoms

- Resident memory went from about 50 MB at start to about 280 MB, and never came back down.
- Each rise lined up with a CPU spike.

Readings from `/proc/$PID/status` and the data directory, before the fix:

| Reading | Value |
|---|---|
| VmRSS (total resident memory) | 215.6 MiB |
| RssAnon (Go heap and runtime) | 91.9 MiB |
| RssFile (memory-mapped files) | 123.8 MiB |
| Open file descriptors | 4,765 |
| Blocks in `$DATA_DIR/tsdb` | 2,357 |

## Diagnosis

**It was not a Go memory leak.** With `MEMORY_LOG=true`, the hourly maintenance line showed the heap flat across hours, and the goroutine count fell:

```
[Maintenance] memory heap_inuse=67.6MiB heap_alloc=63.3MiB sys=83.8MiB  ... goroutines=278
[Maintenance] memory heap_inuse=67.2MiB heap_alloc=62.9MiB sys=147.9MiB ... goroutines=237
```

`sys` rose, but `sys` is memory the Go runtime has ever reserved. It does not shrink even after pages are handed back to the operating system, so it does not track resident memory. The growth was outside the Go heap.

**The growth was memory-mapped files.** `RssFile` was larger than `RssAnon`. The TSDB memory-maps every block's chunk and index files, and keeps about two file descriptors open per block. 4,765 descriptors ≈ 2 × 2,357 blocks.

**The blocks never merged.** Both TSDBs, the agent TSDB and the collector TSDB, were opened with the Prometheus library's `tsdb.DefaultOptions()`. That sets both the minimum and maximum block duration to 2 hours. When the TSDB opens, it drops every merge size larger than the maximum, which leaves only 2 hours, so merging never happens. Every 2 hours a new block was written and stayed open for good:

- The agent TSDB has unlimited retention by default, so its block count grew without end. 2,357 blocks is about 196 days of history.
- The collector TSDB would have levelled off at about 1,080 blocks under its 90-day default retention.

Each new block added mapped memory and open files. Long-range charts had to open and read every block in their range, which explains the CPU spikes and the step-wise rises in memory.

The Prometheus server avoids this by setting the maximum block duration itself. The library default is meant for embedders to override, and certainstats did not.

## Fix

`internal/tsdbopts` sets the maximum block duration the way the Prometheus server does, and both TSDBs use it:

- A tenth of retention, capped at 31 days.
- 31 days when retention is unlimited.
- 7 days when a storage size limit (`STORAGE_MAX_BYTES` or `COLLECTOR_STORAGE_MAX_BYTES`) is set. Reaching the limit deletes the oldest whole block, so this keeps that to about a week of history rather than a month.
- Never below the 2-hour minimum.

Blocks now merge 2 h → 6 h → 18 h → 54 h and so on, up to the cap. Merging is lossless, and existing blocks merge in the background after upgrading.

A test writes 27 two-hour blocks and checks that they merge into 7 with every sample intact. A second test runs the same blocks with the library default and checks they stay at 27, which documents the original bug.

With `MEMORY_LOG=true`, the hourly memory line now includes `tsdb_blocks` and `collector_tsdb_blocks`, so block growth is visible. Without it, logs are unchanged.

## Results

The same server after upgrading and letting the backlog merge:

| Reading | Before | After | Change |
|---|---|---|---|
| Blocks in `tsdb` | 2,357 | 16 | −99% |
| Open file descriptors | 4,765 | 85 | −98% |
| RssFile | 123.8 MiB | 19.6 MiB | −84% |
| RssAnon | 91.9 MiB | 86.9 MiB | about the same |
| VmRSS | 215.6 MiB | 106.5 MiB | −51% |

`RssAnon` hardly changed, which confirms that the Go side was never the problem.

## Lessons

- **Split resident memory before looking for a leak.** `RssAnon` versus `RssFile` in `/proc/$PID/status` shows whether growth is heap or mapped files. A flat Go heap with rising RSS points away from Go code.
- **Count open files.** A descriptor count that grows with uptime is a strong hint of an ever-growing set of files.
- **Check library defaults against how the upstream server uses the library.** `tsdb.DefaultOptions()` is a starting point, not a production setting.
- **Count blocks carefully during a merge.** Shortly after a merge, `ls -d $DATA_DIR/tsdb/01*` also matches work folders ending in `.tmp-for-creation` or `.tmp-for-deletion`. In this case that glob reported 381 while there were only 16 real blocks. Use `ls -d $DATA_DIR/tsdb/01*/ | grep -v tmp | wc -l`.

## Checking a server

```sh
ls -d $DATA_DIR/tsdb/01*/ | grep -v tmp | wc -l            # blocks; tens, not thousands
ls -d $DATA_DIR/collector_tsdb/01*/ | grep -v tmp | wc -l
ls /proc/$PID/fd | wc -l                                    # open files
grep -E 'VmRSS|RssAnon|RssFile' /proc/$PID/status
```

On a healthy server, the block counts stay roughly constant over days. `RssFile` and the open-file count stay flat instead of growing with uptime.

See [security and rollout](security-and-rollout.md) for upgrade steps and [environment](environment.md) for the retention and size settings.
