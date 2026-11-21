# Custom storage formats

This page describes certainstats' two own on-disk formats, `tblk` and `rblk`: what each one stores, how they differ, what they share, and how to work on them. It is for developers. Everything else the server stores lives in SQLite or a Prometheus TSDB; see [architecture](architecture.md) and [collector internals](collectors/README.md#storage).

## Two packages, two formats

`internal/tblk` and `internal/rblk` are separate packages. Neither imports the other. Both are named after their file format, so an import path shows that the code reads and writes a format we own, not a general library.

| | `internal/tblk` | `internal/rblk` |
| --- | --- | --- |
| Stores | **Counts**: rows of keys and summed measures per minute, hour and day. The busiest rows are kept and the rest folded into "other" rows. | **Records**: the rows themselves (an alert, its fields and its child decisions) |
| Answers | Totals, top lists, timelines, filters, group-by, series | Lists with filters, search, sort and pages; detail by id; counts and groups over rows; children still active; retention |
| Block files | `blocks/<grain>/<start>-<end>-<seq>[-m].blk`, magic `TNB1` | `blocks/<day start>-<seq>.rblk`, magic `RBK1` |
| What one block holds | One time slice of one grain: 15 minutes, an hour or a day | One UTC day of every owner's rows |
| Status | In production behind an opt-in: Knot with `KNOT_ENGINE=topn` | In production behind an opt-in: CrowdSec with `CROWDSEC_ENGINE=rblk`, where it holds all alert data with no rollup beside it. Only the 30-day study needs the `rblkreport` build tag. |
| Used by | Knot DNS dnstap ([Knot internals](collectors/knot.md)) | CrowdSec alerts and decisions ([CrowdSec internals](collectors/crowdsec.md#record-engine-crowdsec_enginerblk)) |

The names users see don't follow the package rename. The Knot setting is still `KNOT_ENGINE=topn`, and its data folder is still `knot-topn/`. Knot's own types are still `TopN` and `OpenTopN`, because they describe top-N counting, not the package. Changing those would break existing data folders and configuration.

## What they share

Both formats follow the same pattern. When you change one, check whether the other needs the same change.

- **SQLite head, sealed blocks.** New data goes into a small SQLite file, `head.db`. Once it has settled, two hours after the fact, it is sealed into read-only block files and removed from the head.
- **Sections.** A block is a set of zstd-compressed sections, each checked by a CRC32 of its compressed bytes, plus a trailer that ends with the magic. A section that fails its check makes the block unreadable, and the block is set aside rather than read wrong.
- **A sorted dictionary per block.** Repeated strings are stored once per block in sorted order, and rows hold their index. Because the order is sorted, comparing indexes compares strings. A filter value missing from the dictionary skips the whole block.
- **Write first, then swap.** A new or merged block is written in full under a higher sequence number before the old one is deleted. On open, the highest sequence for a slice wins, so a crash between the two steps never loses or doubles a row.
- **Retention deletes files.** Each block stands alone, with its own dictionary, so expiring old data removes files and needs no global sweep.

The low-level pieces exist twice for now: zstd encoders and decoders, the CRC checks and the section reader. `rblk` copied them from `tblk` to build the study quickly. Now that both are in production, moving them into one shared package that both import is the next cleanup.

## `tblk`: counts

- **Writes:**
  - `Append` replaces a minute's rows by key. Knot uses it, because collectors resend whole minutes.
  - `Add` sums rows in, for sources whose events arrive a few at a time. A late minute still reaches any hour or day already built from it.
- **Tiers:**
  - Settled 15-minute slices are sealed from the head.
  - Four slices merge into an hour, with the top N per collector applied.
  - A complete day's hours merge into a day.
- **Folding:** below the cap, a row's folding keys become "other", so sums still add up.
  - With fold levels (`Key.Level`), dependent keys fold in order: an IP folds first, then its range, then its ASN.
  - `Result.Folded` is set when the key a query reads may have been folded.
  - **Bounded folding** (`Family.Bound`): the cap keeps only rows strictly above the first row past it, so ties fold together. Each cut is added to a bound row (every key "other"). Summing the bound measure over a range gives how far any key's sum can be below the truth. Knot's name and client families use it; a network key can fold too. See the [tblk spec](specs/tblk.md#721-bounded-folding).
- **Reference engine:** an SQL engine in one SQLite file holds the same data. The tests check the block engine against it.

- **Store rules:** a store records each family's shape and refuses to open if a family's keys, measures or folding changed (`ErrShapeChanged`). Give a new shape a new family name. A store has one writer, held by a `flock` on its `lock` file (`ErrLocked`). Rows the writer refuses (the wrong number of keys or measures, a bad network or number, `(other)` in a key that doesn't fold, a negative count in a bounded family) are counted per family in `Stats.Dropped`.
- **Reader checks:** a block is set aside as damaged unless every section is one zstd frame within bounds, no sections overlap, the dictionary is sorted, and every index and bucket is in range. See [Reading a Block](specs/tblk.md#9-reading-a-block).

The byte-level specification is [specs/tblk.md](specs/tblk.md), revision 2. The package overview is in its `doc.go`.

## `rblk`: records

- **Schema:** a type declares its columns, each of one kind:
  - `Dict`: repeated strings, kept in the dictionary
  - `Int` or `Bool`
  - `Text`: free text, stored only when a template can't rebuild it
  - `ID`: a UUID takes 16 bytes

  Columns can also carry:
  - `SameAs`: stored as 0 when equal to another column
  - `Index`: an index in the head, and in the slim engine
  - a dedupe column, and child columns with one `Active` expiry column
- **Sealing:** rows older than two hours are sealed by the hour into their day's block. Today's block is rewritten as each hour settles, so the head holds about two hours of rows.
  - The block header's `upto` field records where sealing stopped. After a crash, only that part is merged again.
  - Late rows for a sealed day go to the head and are merged into the day's block on the next pass.
- **Reads:**
  - A query reads the head in SQL and the blocks by column.
  - Each block's codes for a column are resolved once, then filters and search are lookups by code.
  - Pages keep only the rows that could make the page.
  - A time-sorted page counts days it can't reach from their headers without reading them.
  - Decoded sections stay cached across queries, up to 64 MB.
- **Two engines:** `Blocks` is the format described here. `Slim` is a single SQLite file with strings as integer ids. It is the head of `Blocks`, and the alternative the study measured.

The byte-level specification is [specs/rblk.md](specs/rblk.md). The package overview is in its `doc.go`.

### SQLite traps found in the slim head

These apply to any SQLite schema that stores strings as integer ids:

- **Sorting by a string column through the string table's index** (string table first, then the column's index) helps only when the query has no filters. With a filter, SQLite may probe the time index once per string. Use that plan only without filters, and force the column's index with `INDEXED BY`.
- **An index on a column used in GROUP BY lets SQLite skip the sort by walking that index across every row of the owner**, ignoring the index the WHERE clause would use. A unary `+` on the group key (`GROUP BY +c_scenario`) prevents it. One IP's breakdown by scenario went from 3 ms to 550 ms without it.

## Testing

- `go test ./internal/tblk/` checks the block engine against the SQL reference engine across tiers, retention and the size cap. It also covers crash cleanup and byte-identical block writes. It takes several minutes.
- `go test ./internal/rblk/` checks both engines against a brute-force reference, through:
  - inserts, duplicates and sealing
  - late rows, retention and owner removal
  - a crash during sealing, and a damaged block
- `go test -run Rblk ./internal/collector/type/crowdsec/` checks the CrowdSec `Store` on rblk against the same `Store` on `crowdsec.db`, on every screen with coverage, through sealing, duplicates, late alerts, retention and a removed account.
- The 30-day study (`go test -tags rblkreport -run TestRecordsReport …`) measures size, write cost, memory and every screen's time; see below.
- The 30-day measurements, their method and results are in `artifacts/crowdsec-records-2026-11-20/` for `rblk`. For `tblk`, see [Knot internals](collectors/knot.md#tests).

## Adding a format

A new on-disk format gets its own package, named after the format and its file extension, as `tblk` and `rblk` are. Its `doc.go` opens by saying it is certainstats' own format, with the file extension and magic. Keep settings, folder names and file extensions stable once released, even if the package is later renamed.
