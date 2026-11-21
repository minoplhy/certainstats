// Package rblk is certainstats' own record block format (.rblk files, magic
// RBK1), not a general-purpose library. It stores records over time (alerts,
// log entries) compactly and answers list, detail, search and count screens
// over them. It is the companion to tblk, which keeps counts: rblk keeps the
// rows themselves.
//
// A collector type declares a schema: repeated strings (Dict columns, kept
// once in a dictionary), numbers, free text a template can usually rebuild,
// identifiers such as UUIDs, and child rows (an alert's decisions). It gets
// insert with dedupe, get by id, find (filters, substring search over Dict
// columns, sort, page, total), aggregate (group, bucket, sums, distinct
// counts), active children (expiry after now), retention and owner removal.
//
// Two engines implement it: Blocks (a Slim head for the last two hours plus
// one immutable, zstd-compressed column block per day, sealed by the hour) is
// the one products use; CrowdSec opts in with CROWDSEC_ENGINE=rblk. Slim (one
// SQLite file with strings as integer ids) is Blocks' head and the
// alternative the records study measured. The 30-day study itself builds
// only with the rblkreport tag (artifacts/crowdsec-records-2026-11-20/).
package rblk
