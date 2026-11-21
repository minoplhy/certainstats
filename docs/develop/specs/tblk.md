# The tblk Count Block Format, Version 1

```
certainstats                                                  2026-11-21
Specification: tblk-1, revision 2                    Status: Implemented
```

## Abstract

This document specifies tblk version 1. It is the on-disk format certainstats uses for counts over time, broken down by a few keys: for example DNS queries per zone, name, type and response code per minute, hour and day. A tblk store is a directory with a SQLite head for the newest data and immutable block files for everything sealed. This document defines the block file byte for byte. It also defines the file names and the rules that decide which block files are live. The head database is described only as far as a reader of blocks needs.

## Status of This Document

This document describes the format as implemented by `internal/tblk` (files `blockformat.go`, `blockengine.go`, `schema.go`, `netkey.go`, `store.go`, `sqlengine.go`). Where this document and the code disagree, the code defines the format, and this document has a bug.

**Revision 2** changes no byte of the format: every block a revision 1 writer produced is still valid. (It also fixes a writer bug: keys were joined with a 0x00 byte to tell rows apart, so two different key lists that joined to the same bytes were summed as one row.) It specifies what revision 1 left open: the unbounded folding algorithm, the reserved value, the numeric rules of bounded folding, string and integer keys, `covered`, and the exact checks a reader makes. It adds two store rules: each family's shape is recorded and checked (Section 8.4), and a store has one writer (Section 8.5).

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conventions and Terminology](#2-conventions-and-terminology)
3. [Data Model](#3-data-model)
4. [Primitive Encodings](#4-primitive-encodings)
5. [Block File Layout](#5-block-file-layout)
6. [Sections](#6-sections)
7. [Row Order and Content Rules](#7-row-order-and-content-rules)
8. [Store Directory and File Names](#8-store-directory-and-file-names)
9. [Reading a Block](#9-reading-a-block)
10. [Writing a Block](#10-writing-a-block)
11. [Versioning](#11-versioning)
12. [Security Considerations](#12-security-considerations)
13. [References](#13-references)
- [Appendix A. Worked Example](#appendix-a-worked-example)
- [Appendix B. Relation to rblk](#appendix-b-relation-to-rblk)

## 1. Introduction

Counts with unbounded keys, such as DNS names or client networks, do not fit a time-series database: every distinct key would be a new series. tblk stores them as rows of (collector, time bucket, keys, measures). Within each period only the busiest rows are kept, and the rest are folded into "other" rows, so sums still add up.

A block holds the rows of one time span at one grain: minutes, hours or days. Every block carries its own sorted dictionary of strings and one compressed section per column. So:

- a block can be read without any other file;
- retention deletes whole files;
- a filter value missing from a block's dictionary skips the block without decoding its rows.

## 2. Conventions and Terminology

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in BCP 14 [RFC2119] [RFC8174] when, and only when, they appear in all capitals, as shown here.

**Owner**
: An account. Its rows are kept apart from every other owner's.

**Collector**
: The source a row came from, within an owner.

**Family**
: A named table shape, declared by the program that uses tblk: an ordered list of keys and an ordered list of measures. Families are not stored in block files (see Section 3).

**Key**
: A string column of a family. A key has a kind (string, network or integer) and may fold.

**Measure**
: A signed 64-bit integer column of a family that is summed. Sums stop at the int64 limits instead of wrapping (Section 3).

**Grain**
: The bucket width of a block: minute (60,000 ms), hour (3,600,000 ms) or day (86,400,000 ms).

**Bucket**
: The start of a grain-wide period, in milliseconds since the Unix epoch, UTC.

**Group**
: The rows of one (owner, family) pair inside one block.

**Fold**
: Replacing a key's value with the string `(other)` when a row falls below the cap for its period.

**Section**
: One zstd-compressed, CRC-checked byte range of a block file.

Times are milliseconds since 1970-01-01T00:00:00Z, as signed 64-bit integers.

## 3. Data Model

A program using tblk declares a schema:

- a list of families, each with ordered keys and ordered measures;
- per family, optional caps per grain (how many rows per collector a period keeps). A cap is an integer of at least 1; a grain with no cap, or a cap of 0 or less, keeps every row, as certainstats does;
- per family, optionally a **bound measure**, which makes folding bounded (Section 7.2.1).

**The schema is not stored in block files.** A block names each group's family. The number and order of its columns come from that family's declaration. A reader MUST be given the same family declarations the writer used. A reader that finds a group whose column count doesn't match its family (Section 9) MUST treat the block as damaged. A matching column count does not prove a matching shape (keys or measures could have been reordered), so a store records each family's shape and refuses to open with a different one (Section 8.4).

Each row has:

- **collector**: a string;
- **bucket**: a time, aligned to the block's grain;
- **keys**: one string per family key, in declaration order;
- **measures**: one signed 64-bit integer per family measure, in declaration order.

Family, key and measure **names** match `[a-z][a-z0-9_]{0,31}`: a lower-case letter, then up to 31 lower-case letters, digits or underscores. A store refuses a schema with any other name.

Strings (owner, family, collector and key values) are byte strings. tblk does not check that they are UTF-8 and does not normalize them; two strings are equal when their bytes are, and they sort by bytes. Programs SHOULD write UTF-8.

Key values by kind:

- **String keys** hold any string.
- **Network keys** hold the fixed-width form of Section 7.3, never the CIDR text, or `(other)`.
- **Integer keys** hold an optional `+` or `-` followed by decimal digits, whose value fits in a signed 64-bit integer. They are stored exactly as written and not normalized, so `1`, `01` and `+1` are three values, and they sort as strings (by bytes), not by number.
- **Folding keys** are string or network keys. Integer keys do not fold.

**The reserved value** is `(other)` (bytes `28 6F 74 68 65 72 29`). It marks a folded key, and nothing in a block tells a folded key from a real value with the same bytes. So:

- a key that does not fold MUST NOT hold `(other)`, except in a bounded family's bound row (Section 7.2.1), where every key holds it;
- a program MUST NOT store `(other)` as a real value in a folding key, because it would merge with folded rows. A program whose data could contain it escapes it first;
- certainstats' writer drops a row that breaks the first rule and counts it in `Stats.Dropped`, per family. It cannot detect the second.

`Stats.Dropped` counts, per family, every row certainstats' writer refuses, whatever the reason: the wrong number of keys or measures, a network or number key that does not parse, `(other)` in a key that does not fold (other than a bound row), or a negative first or bound measure in a bounded family (Section 7.2.1). It counts from when the store opened and is not kept across restarts.

**Measures** are signed. When rows are summed (merging, folding, reading), a sum that would leave the int64 range stops at the limit (`2^63-1` or `-2^63`) instead of wrapping. Counts that large are far outside tblk's use. Saturating addition is not associative once positive and negative values mix: if a partial sum hits a limit, the final sum can depend on how rows were grouped, and tblk sums in several stages (merging, folding, rollup, reading). A sum is exact, whatever the grouping, only while every partial sum stays inside the int64 range. A program that needs the same answer from every path SHOULD keep its measures far from the limits. For values that are all at least 0, as the first and bound measures of a bounded family are, saturating sums are associative. The SQL reference engine reports an error instead where SQLite does the sum.

## 4. Primitive Encodings

### 4.1. Fixed-width integers

Fixed-width integers are little-endian. `u32` is an unsigned 32-bit integer. `u64` is unsigned 64-bit. `i64` is a two's-complement signed 64-bit integer.

### 4.2. uvarint

An unsigned integer is written in base-128 groups of 7 bits, least significant group first. Each byte except the last has its high bit (0x80) set. The encoding is the same as Protocol Buffers varints [PROTOBUF] and Go's `encoding/binary` `Uvarint`. An encoding MUST NOT exceed 10 bytes. Writers MUST use the shortest encoding.

### 4.3. varint (signed)

A signed integer `v` is ZigZag-mapped to `(v << 1) XOR (v >> 63)` (arithmetic shift), then written as a uvarint. This is Go's `binary.AppendVarint`.

### 4.4. string

A string is a uvarint byte length followed by that many bytes, with no terminator.

### 4.5. Section reference

A section reference is a uvarint offset (from the start of the file), a uvarint compressed length, and a u32 CRC.

```
section-ref = uvarint(off) uvarint(n) u32(crc)
```

### 4.6. CRC

CRC is CRC-32 with the IEEE 802.3 polynomial 0xEDB88320 (reflected), initial value 0xFFFFFFFF and final XOR 0xFFFFFFFF. It is the CRC of zlib, gzip and PNG.

### 4.7. Compression

Every section's stored bytes are exactly one Zstandard frame [RFC8878], filling the section: its header, its blocks up to the one marked last, and its 4-byte checksum if the header declares one, with no bytes, skippable frames or further frames after it. The frame decompresses to the section's raw bytes. The frame MAY omit its content size (certainstats' writer omits it for small sections). Writers MAY use any compression level. Readers MUST NOT depend on a dictionary or on frame options beyond [RFC8878].

## 5. Block File Layout

```
+---------------------------+ offset 0
| header         (32 bytes) |
+---------------------------+ offset 32
| dictionary section        |
| column sections ...       |   zstd frames, back to back
| index section             |
+---------------------------+ offset size-24
| trailer        (24 bytes) |
+---------------------------+ offset size
```

### 5.1. Header

| Offset | Size | Field | Value |
|---:|---:|---|---|
| 0 | 4 | magic | ASCII `TNB1` (`54 4E 42 31`) |
| 4 | 1 | version | `1` |
| 5 | 1 | grain | `0` minute, `1` hour, `2` day |
| 6 | 2 | spare | Writers MUST write zero; readers MUST ignore |
| 8 | 8 | start | `i64`, the first bucket the block may hold |
| 16 | 8 | end | `i64`, the end of the span, exclusive |
| 24 | 8 | covered | `i64`, see below |

`start` and `end` MUST be multiples of the grain's width, and `start < end`. Every row's bucket lies in `[start, end)`.

`covered` is the time up to which the block's source data was complete when the block was written. It MUST satisfy `start < covered <= end`. Writers set it to `end` for every new block, a merge included: a merge or tier block is only written from complete input. A program that rewrites one block in place (to drop an owner, for example) MUST copy `covered` from the block it replaces. Readers use the largest `covered` of a tier's blocks to know how far that tier is built. A writer builds each tier in time order, without gaps, so a tier's coverage is contiguous: everything before its largest `covered` is built (older blocks may since have been removed by retention).

The span `(end - start) / grain width` MUST NOT exceed 2^32 buckets.

### 5.2. Trailer

| Offset (from size-24) | Size | Field |
|---:|---:|---|
| 0 | 8 | index offset, `u64` |
| 8 | 4 | index compressed length, `u32` |
| 12 | 4 | index CRC, `u32`, of the index section's stored bytes |
| 16 | 4 | header CRC, `u32`, of the 32 header bytes |
| 20 | 4 | magic, ASCII `TNB1` |

The header CRC covers bytes 0 to 31 exactly as stored, magic and spare bytes included.

### 5.3. Sections

Sections lie between the header and the trailer. Writers place the dictionary first, then each group's columns in group order, then the index last, with no gaps. Readers MUST locate sections only through the index and its references, never by position.

- Every section is at least 1 byte long.
- The index lies in `[32, size - 24)`.
- The dictionary and every column lie in `[32, index offset)`, and no two of them overlap.
- Readers MAY accept gaps between sections.

## 6. Sections

### 6.1. Index

The index is a section whose raw bytes are:

```
index        = section-ref(dict)
               uvarint(ndict)              ; number of dictionary strings
               uvarint(ngroups)
               ngroups * group
group        = string(owner) string(family)
               uvarint(rows)               ; rows in the group
               uvarint(folded)             ; of those, rows that are folded
               uvarint(ncols)
               ncols * section-ref         ; columns, in the order of 6.3
```

The index's raw bytes MUST hold exactly this, with nothing after it. Groups MUST be sorted by owner, then by family, both ascending by bytes. An (owner, family) pair MUST NOT appear twice. `rows` MUST be at least 1. `folded` MUST NOT exceed `rows`: it is the number of the group's rows whose family's first level-1 folding key holds `(other)`, and 0 for families with no folding key.

### 6.2. Dictionary

```
dictionary   = uvarint(count) count * string
```

`count` MUST equal the index's `ndict`, and the raw bytes MUST hold nothing after the last string. The strings MUST be distinct and sorted ascending by bytes. They are every collector and key value used by any row of the block, and nothing else.

A dictionary index is a string's 0-based position in this list. Because the list is sorted, comparing indexes compares strings. So a prefix or range filter over a key is a range of indexes.

### 6.3. Columns of a group

A group of a family with `K` keys and `M` measures has `ncols = 2 + K + M` columns, in this order. Each decodes to exactly `rows` values.

| # | Column | Raw encoding |
|---|---|---|
| 1 | collector | Run-length pairs `uvarint(index) uvarint(run)`. The runs sum to exactly `rows`. A run MUST be at least 1, and adjacent pairs SHOULD NOT repeat an index. |
| 2 | bucket | `rows` uvarints, each `(bucket - start) / grain width`, below the block's span |
| 3 to 2+K | key `i` | `rows` uvarints, dictionary indexes |

Every dictionary index, in the collector and key columns, MUST be below `ndict`.
| 3+K to 2+K+M | measure `j` | `rows` varints (Section 4.3) |

A column's raw bytes MUST hold exactly its values, with nothing after them.

## 7. Row Order and Content Rules

### 7.1. Order and uniqueness

Within a group, rows MUST be sorted by collector, then bucket, then keys in declaration order. Each is compared as a string by bytes, or as a number for bucket. Two rows MUST NOT share collector, bucket and all keys. Writers sum such rows' measures into one row.

### 7.2. Folding

A cap *k* applies per collector and per period of the block's grain, when finer rows are summed into that period. Keys that don't fold keep their values, so folded rows still split by them. Measures are never dropped, so the sum of a measure over a group equals the sum of what was written.

For a family that is not bounded, with fold levels 1 to *L*:

Each collector and each bucket of the coarser period is folded on its own: rows are never summed or ranked across collectors or buckets. Within one collector and bucket:

1. Sort each row into a stage by the levels it has already lost. A row is in stage *i* when its keys of levels 1 to *i* are `(other)` and, if *i* < *L*, its level *i*+1 keys are not. A row from a finer period may arrive already folded. Rows with exactly the same keys in a stage are summed.
2. For *i* from 0 to *L*-1, rank stage *i* by the first measure, largest first, comparing signed values. Break ties by comparing the keys one at a time in declaration order, each by bytes; a key list ends the comparison at its first differing key. Keep the first *k* rows exactly as they are. Set the level *i*+1 keys of every other row to `(other)` and move it into stage *i*+1, summing rows that now share keys.
3. Keep every row of stage *L*. These have lost every level.

So each stage keeps at most *k* named rows, and rows tied at the cap split by key order. A family with one level keeps its top *k* rows plus `(other)` rows. Bounded families use the rule below instead.

#### 7.2.1. Bounded folding

A family MAY name one of its measures, other than the first, as its **bound measure**. A bounded family MUST have exactly one fold level and only string or network keys. Its cap works differently, so the error it introduces is known:

The first measure and the bound measure of every row MUST be at least 0. certainstats' writer drops a row that breaks this and counts it in `Stats.Dropped`. A negative count would void the bound below, because a period's total could then be smaller than *k*+1 times its cut.

1. Per collector and period (each on its own, as above), take the rows whose folding keys are not `(other)`, ranked by the first measure, largest first. If there are more than *k* (the cap), the **cut** is the first measure of the row at position *k*+1. Otherwise the cut is 0.
2. Rows whose first measure is greater than the cut keep their keys. Every other row, including rows tied with the cut, has its folding keys set to `(other)` and is summed. Ties therefore fold together, whatever their key values.
3. The **bound row** has every key, folding or not, set to `(other)`. Its first measure is 0. Its bound measure accumulates cuts: when rows of a finer period are summed into a coarser one, their bound rows are summed too, and the new period's cut is added. There is at most one bound row per collector and period. It is omitted when its bound measure would be 0 and no input carried one. When every key of the family folds, the bound row and the folded row have the same keys and are one row: its first measure is the folded sum and its bound measure the error.
4. Writers MAY put their own earlier cuts (for example, a collector that cut its data before sending it) on the bound row of each minute.

For a range of buckets, let *E* be the sum of the bound measure over the bound rows read for it, for the collector or collectors asked about. Then for every key value, the true sum of its **first measure** lies between the sum read and that sum plus *E*. The bound says nothing about other measures. A sum that reaches the int64 limit (Section 3) voids it. A cut at cap *k* is at most 1/(*k*+1) of its period's total for that collector, since the *k*+1 rows at or above it are all at least that large. Summing over every cut that the range's bound rows carry, stage *j* (for example the collector's own, then hour, then day) and period *p* of that stage:

> *E* ≤ Σⱼ Σₚ *N*ⱼ,ₚ / (*k*ⱼ,ₚ + 1)

where *N*ⱼ,ₚ is that period's total of the first measure for the collector and *k*ⱼ,ₚ is the cap the cut was made at. Caps may differ per grain and may change over time (Section 8.4), so one stage can hold cuts made at different caps. When each stage has one cap *k*ⱼ over the whole range, this becomes *E* ≤ *N*·Σⱼ 1/(*k*ⱼ+1), where *N* is the range's total; with one cap *k* throughout, *E* ≤ *s*·*N*/(*k*+1) for *s* stages. Blocks do not store caps, so these estimates need the cap history from the program's configuration. *E* itself does not: it is read from the bound rows and is the bound a reader reports. An earlier cut a writer puts on a minute's bound row (step 4) counts as a stage only if it was made by this rule, at a known cap. Unlike the classic Misra–Gries summary, kept rows are not reduced by the cut: a kept row's count is exact for its period.

The bound row is an ordinary row in the block. A reader that doesn't know about bounded folding reads it as a folded row with a first measure of 0, which changes no sum.

### 7.3. Network keys

A network key is stored as 36 bytes of ASCII:

- 32 lowercase hex digits of the 16-byte network address, with IPv4 mapped to `::ffff:a.b.c.d`;
- `/`;
- the prefix length as 3 decimal digits, counted in IPv6 form: `000` to `128`, and `096` to `128` for an IPv4 network (its length plus 96).

The address MUST be masked to its prefix. An IPv6 network inside `::ffff:0:0/96` has the same key as the IPv4 network it maps, and reads back as IPv4. Because the width is fixed, byte order equals address order. All networks inside a CIDR then form one contiguous key range.

Example: `192.0.2.0/24` is `00000000000000000000ffffc0000200/120`.

A folded network is stored as `(other)`. It sorts before every network key (`(` is below every hex digit), so no CIDR range contains it.

## 8. Store Directory and File Names

```
<store>/
  head.db                 SQLite: unsealed minutes and metadata
  blocks/minute/*.blk
  blocks/hour/*.blk
  blocks/day/*.blk
```

### 8.1. File names

A block is named `<start>-<end>-<seq>.blk`, or `<start>-<end>-<seq>-m.blk` when it is a merge:

- `start` and `end` repeat the header in decimal;
- `seq` is a decimal sequence number, unique within the store and increasing with each write;
- `-m` marks a block that replaces every block it covers.

Names `<start>-<end>.blk` from before sequence numbers have `seq` 0. A block's directory MUST match its header's grain. A block in the wrong directory is damaged.

### 8.2. Which blocks are live

A block B is **superseded** when another block M in the same directory has all of the following:

- M is a merge (`-m`);
- M's `seq` is greater than B's;
- M's span covers B's span (`M.start <= B.start` and `B.end <= M.end`).

On open, a reader MUST ignore superseded blocks, and the store's owner SHOULD delete them. They are left over from a merge interrupted after writing its output and before deleting its inputs. Files ending in `.tmp` are unfinished writes. Readers MUST ignore them, and the owner SHOULD delete them.

### 8.3. Tiers (informative)

The certainstats engine builds blocks in three steps:

1. Every settled 15-minute slice (two hours after the fact) is sealed from `head.db` into a minute block.
2. Four slices merge into an hour-long minute block. The same pass writes an hour block with the cap applied. A quiet hour whose minutes fit one slice keeps that slice as its minute block; it counts as folded once the hour block exists.
3. A complete day's hour blocks merge into a day block.

Retention and the size cap delete whole files, oldest and most detailed first. `head.db` holds the unsealed minutes and `sealed_until`, the time before which every minute is in blocks. Its layout belongs to the implementation and is not part of this format.

### 8.4. Family shapes

Because blocks do not carry the schema, a store records each family's **shape** the first time it opens with that family, and MUST refuse to open when a declared family's shape differs from the recorded one. certainstats records it in the `meta` table of the store's SQLite file (`head.db`, or `topn.db` with the SQL engine) under the key `shape:<family>`, as the FNV-1a 64-bit hash of this text:

```
<name>|k:<key>:<kind>:<fold>:<level>|...|m:<measure>|...|b:<bound measure>
```

`kind` is 0 (string), 1 (network) or 2 (integer). `fold` is `true` or `false`. `level` is as declared, 0 meaning 1. Keys and measures appear in declaration order.

- Changing a family's keys, kinds, folding, measures or bound measure is refused (`ErrShapeChanged`). Declare the new shape under a new family name, and keep the old family declared while its blocks remain.
- Caps are not part of the shape and may change.
- Families that are recorded but no longer declared are left alone.
- **Stores from before revision 2** have no recorded shapes. The first open records the shapes the program declares and trusts that its blocks were written with them; nothing in the blocks can confirm it. A program that changed a family's shape before upgrading must handle that itself, by renaming the family or rebuilding its blocks. certainstats never changed a released family's shape.

### 8.5. One writer

A store has one writer. certainstats takes an exclusive `flock` on the file `lock` in the store directory for as long as the store is open. A second open fails (`ErrLocked`), from another process or the same one. The lock goes with the file when the process ends, so a crash leaves none behind. On systems without `flock`, keeping to one writer is the program's job.

## 9. Reading a Block

A reader MUST take these steps and treat any failure as a damaged block. A damaged block MUST NOT be used. The store's owner SHOULD move it aside rather than delete it.

On opening a block:

1. The file is at least 56 bytes.
2. Bytes 0–3 and the last 4 bytes are `TNB1`. Byte 4 is `1`.
3. The CRC of bytes 0–31 equals the trailer's header CRC.
4. The header follows Section 5.1: grain is 0, 1 or 2 and matches the directory; `start` and `end` are multiples of the grain width; `start < end`; `start < covered <= end`; the span is at most 2^32 buckets.
5. The index reference from the trailer lies in `[32, size - 24)` (Section 5.3). The index section passes step 8 below, and its raw bytes parse exactly as Section 6.1, with nothing left over: groups sorted and distinct, `rows >= 1`, `folded <= rows`, `ncols >= 2`.
6. `ndict`, `ngroups` and every `ncols` are at most the file size, and every `rows` at most 2^31, before they size anything.
7. Every section reference in the index lies in `[32, index offset)`, is at least 1 byte long, and overlaps no other.

On reading a section (the index on open; the dictionary and columns when a query needs them):

8. Its compressed length is at most 2^31 bytes. The CRC of its stored bytes matches its reference. It is exactly one Zstandard frame (Section 4.7). It decompresses to at most 2^31 bytes.
9. The dictionary: its count equals `ndict`, its strings are distinct and sorted, and nothing follows them.
10. A group's `ncols` equals `2 + K + M` for its family.
11. Every column decodes to exactly `rows` values with nothing after them. Collector runs are at least 1 and sum to exactly `rows`. Collector and key indexes are below `ndict`, and bucket offsets are below the span.

A reader MAY decode sections lazily, only those a query needs. A damaged section it never reads then goes unnoticed.

These checks are for **structural safety**: a block that passes them cannot make a reader read out of range or crash on a bad index or count, and every allocation they lead to is bounded by the limits above. Those limits are still large, for example 2^31 bytes for one section (step 8), so a reader on a small machine can run out of memory on a block that passes; provisioning for the configured limits is the implementation's job. **Full conformance** asks more: rows sorted and distinct (Section 7.1), network keys in the form of Section 7.3, integer keys in the grammar of Section 3, and `(other)` only where Section 3 allows it. These are writer obligations. A reader is not required to check them, and certainstats' reader does not. A block that breaks them still decodes safely, but it can give wrong answers for its own rows. Duplicate or unsorted rows still sum correctly. A malformed network key can fall outside a CIDR filter it should match, and a misplaced `(other)` is counted as folded.

## 10. Writing a Block

1. Sort and merge the rows as in Section 7.1. Build the dictionary from the collectors and key values used. Sort it, and map every value to its index.
2. Reserve 32 bytes for the header. Then write the dictionary section, each group's columns in group order, and the index section last.
3. Fill in the header. Then append the trailer with the index reference and the CRC of the finished header.
4. Write the bytes to `<name>.tmp`, flush them to stable storage (fsync), rename to `<name>`, and sync the directory.
5. When the block replaces others (a merge), only after step 4 delete the blocks it supersedes. That deletion needs no directory sync: if a deleted input comes back after a power cut, the merged block still supersedes it (Section 8.2).

A block deleted with **no** replacement (retention, the size cap, or removing an owner whose rows were all a block held) has nothing to supersede it if its directory entry comes back. The writer MUST sync the block's grain directory after such deletions and before the operation reports success; one sync per directory after all deletions of a pass is enough.

Given the same rows, header and compression level, certainstats' writer produces the same bytes. This holds for one writer and one version of its Zstandard library with the same options: another implementation, or another library version, may compress the same raw bytes into a different but equally valid frame. Tests in `internal/tblk` check it.

## 11. Versioning

The version byte is `1`. A change that older readers would misread MUST change the version byte and SHOULD change the magic. Version 1 readers MUST reject other versions. Revisions of this document that only state rules more exactly keep the version.

Adding a family is a schema change, not a format change. Changing an existing family's shape is refused by the store (Section 8.4), so a program declares the new shape as a new family.

## 12. Security Considerations

Block files are trusted local data. Readers still check them, because disks and crashes damage files:

- **The CRCs detect accidental damage, not tampering.** Anyone who can write the directory can forge a valid block.
- **Decompression can expand a small frame into a large buffer.** Readers MUST bound compressed and decompressed sizes (Section 9, step 8). certainstats sets both limits to 2^31 bytes and its decoder's memory limit to the same.
- **Counts read from a file must not size allocations unchecked.** These are `rows`, `ndict`, `ngroups` and `ncols`. Readers MUST check them before allocating (Section 9, step 6). A column decoder also checks that the raw bytes can hold `rows` values (each takes at least one byte) before allocating them.
- **Indexes read from a file must not reach outside what they index.** Dictionary indexes and bucket offsets are checked against `ndict` and the span (Section 9, step 11). A reader that skipped this could read past its dictionary.
- **The trailer has no checksum of its own.** A damaged index offset or length is very likely to fail the index CRC check, because the CRC is computed over the bytes the damaged reference points at. CRC-32 is a strong check against accidental damage, not a guarantee: a collision is possible.
- **Memory across queries.** The 2^31-byte limit is per section. A query decodes a handful of columns at a time, and a store runs a bounded number of queries at once (`Options.QuerySlots`), so peak decoding memory is about slots × columns × section size. Real sections are kilobytes to a few megabytes. The per-section limit is kept high on purpose: one set too low would put good blocks aside as damaged.
- **Owners' rows share a file but not a group.** A reader serving one owner MUST select groups by owner and MUST NOT return another owner's rows.

## 13. References

- **[RFC2119]** Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", BCP 14, RFC 2119, March 1997.
- **[RFC8174]** Leiba, B., "Ambiguity of Uppercase vs Lowercase in RFC 2119 Key Words", BCP 14, RFC 8174, May 2017.
- **[RFC8878]** Collet, Y. and M. Kucherawy, Ed., "Zstandard Compression and the 'application/zstd' Media Type", RFC 8878, February 2021.
- **[PROTOBUF]** "Protocol Buffers Encoding: Base 128 Varints", https://protobuf.dev/programming-guides/encoding/.

## Appendix A. Worked Example

These are the first 32 and last 24 bytes of a real hour block from the storage study, `1796007600000-1796011200000-4695.blk` (24,285 bytes):

```
offset 0      54 4e 42 31 01 01 00 00 80 1f 65 2a a2 01 00 00
offset 16     00 0e 9c 2a a2 01 00 00 00 0e 9c 2a a2 01 00 00

offset 24261  67 5d 00 00 00 00 00 00 5e 01 00 00 0e 2d 4f b8
offset 24277  78 8e fe c2 54 4e 42 31
```

| Bytes | Meaning |
|---|---|
| `54 4e 42 31` | magic `TNB1` |
| `01` | version 1 |
| `01` | grain: hour |
| `00 00` | spare |
| `80 1f 65 2a a2 01 00 00` | start = 1796007600000 (2026-11-30T03:00:00Z) |
| `00 0e 9c 2a a2 01 00 00` | end = 1796011200000, one hour later |
| `00 0e 9c 2a a2 01 00 00` | covered = end |
| `67 5d 00 00 00 00 00 00` | index at offset 23,911 |
| `5e 01 00 00` | index is 350 bytes compressed |
| `0e 2d 4f b8` | index CRC 0xB84F2D0E |
| `78 8e fe c2` | header CRC 0xC2FE8E78 |
| `54 4e 42 31` | magic `TNB1` |

## Appendix B. Relation to rblk

rblk ([rblk.md](rblk.md)) uses the same building blocks:

- zstd sections with CRC-32;
- a sorted dictionary per block;
- write to `.tmp`, then rename, and a sequence number to decide which file wins.

Its layout differs:

- a JSON header at the end instead of a binary header and index;
- dictionary codes offset by one, so 0 can mean "same as another column";
- one block per day holding every owner.

The two formats share no code yet. See [custom storage formats](../storage-formats.md).
