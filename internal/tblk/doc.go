// Package tblk is certainstats' own block format for counts ("topn blocks":
// .blk files, magic TNB1), not a general-purpose library. It stores counts
// over time broken down by a few keys, keeping the busiest rows of each
// period and folding the rest into Other rows, so sums
// still add up. It is the storage for collector data a TSDB cannot hold:
// unbounded keys such as DNS names, client networks, URL paths or ASNs.
//
// A collector type declares families (its keys, summed measures and caps) and
// gets, for each owner and collector:
//   - minute rows, rolled into hour and day tiers with the top-N fold,
//   - retention per tier and a size cap (oldest, most detailed data first),
//   - one query API with tier routing, filters, group-by and series,
//   - bounded reads: a few query slots, then ErrBusy, as the collector TSDB.
//
// Writes come two ways. Append replaces a minute's rows by key, for sources
// that send whole minutes and may resend them (Knot dnstap); a minute older
// than what is already sealed is dropped. Add sums rows in, for sources whose
// events arrive a few at a time (alerts, log lines); a late minute is counted
// in the minute tier and in any hour or day already built from it.
//
// Folding keys that depend on each other take levels (Key.Level): below the
// cap a row loses its level-1 keys first and keeps the deeper ones (an IP
// folds but keeps its range and ASN), and so on level by level. Result.Folded
// is set when the key a query groups or filters by may have been folded.
//
// Filters match a value or a list of values, or exclude them (Not); the key
// "collector" filters and groups by the collector rows came from.
//
// Two engines implement it: Blocks (a SQLite head for unsealed minutes plus
// immutable, zstd-compressed column blocks, merged in levels like a TSDB) is
// the one products use; SQL (one SQLite file, integer keys, a time-first
// primary key) is the reference the tests check Blocks against.
package tblk
