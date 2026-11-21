# The tblk Count Block Format, Version 1

```
certainstats                                                  2026-11-20
Specification: tblk-1                                Status: Implemented
```

## Abstract

This document specifies tblk version 1. It is the on-disk format certainstats uses for counts over time, broken down by a few keys: for example DNS queries per zone, name, type and response code per minute, hour and day. A tblk store is a directory with a SQLite head for the newest data and immutable block files for everything sealed. This document defines the block file byte for byte. It also defines the file names and the rules that decide which block files are live. The head database is described only as far as a reader of blocks needs.

## Status of This Document

This document describes the format as implemented by `internal/tblk` (files `blockformat.go`, `blockengine.go`, `schema.go`, `netkey.go`). Where this document and the code disagree, the code defines the format, and this document has a bug.

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
: A signed 64-bit integer column of a family that is summed.

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
- per family, optional caps per grain (how many rows per collector a period keeps);
- per family, optionally a **bound measure**, which makes folding bounded (Section 7.2.1).

**The schema is not stored in block files.** A block names each group's family. The number and order of its columns come from that family's declaration. A reader MUST be given the same family declarations the writer used. A reader that finds a group whose column count doesn't match its family (Section 9) MUST treat the block as damaged.

Each row has:

- **collector**: a string;
- **bucket**: a time, aligned to the block's grain;
- **keys**: one string per family key, in declaration order;
- **measures**: one signed 64-bit integer per family measure, in declaration order.

Key values by kind:

- **String keys** hold any UTF-8 text.
- **Network keys** hold the fixed-width form of Section 7.3, never the CIDR text, or `(other)`.
- **Integer keys** hold a decimal integer as text, exactly as written; they are not normalized.
- **Any folding key** MAY hold the reserved value `(other)` (bytes `28 6F 74 68 65 72 29`). Folding keys are string or network keys; integer keys do not fold.
- **In a bounded family**, every key of the bound row holds `(other)`, non-folding keys included (Section 7.2.1). No other row has `(other)` in a non-folding key.

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

Every section's stored bytes are exactly one Zstandard frame [RFC8878]. The frame decompresses to the section's raw bytes. Writers MAY use any compression level. Readers MUST NOT depend on a dictionary or on frame options beyond [RFC8878].

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

`covered` is the time up to which the block's source data was complete when the block was written. Version 1 writers set it to `end`. A program that rewrites one block (to apply retention, for example) MUST copy `covered` from the block it replaces. Readers use the largest `covered` of a tier's blocks to know how far that tier is built.

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

Sections lie between the header and the trailer. Writers place the dictionary first, then each group's columns in group order, then the index last. Readers MUST locate sections only through the index and its references, never by position.

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

Groups MUST be sorted by owner, then by family, both ascending by bytes. An (owner, family) pair MUST NOT appear twice. `rows` MUST be at least 1. A group's `folded` count is the number of its rows whose family's first level-1 folding key holds `(other)`. It is 0 for families with no folding key.

### 6.2. Dictionary

```
dictionary   = uvarint(count) count * string
```

`count` MUST equal the index's `ndict`. The strings MUST be distinct and sorted ascending by bytes. They are every collector and key value used by any row of the block, and nothing else.

A dictionary index is a string's 0-based position in this list. Because the list is sorted, comparing indexes compares strings. So a prefix or range filter over a key is a range of indexes.

### 6.3. Columns of a group

A group of a family with `K` keys and `M` measures has `ncols = 2 + K + M` columns, in this order. Each decodes to exactly `rows` values.

| # | Column | Raw encoding |
|---|---|---|
| 1 | collector | Run-length pairs `uvarint(index) uvarint(run)`. The runs sum to `rows`. A run MUST be at least 1, and adjacent pairs SHOULD NOT repeat an index. |
| 2 | bucket | `rows` uvarints, each `(bucket - start) / grain width` |
| 3 to 2+K | key `i` | `rows` uvarints, dictionary indexes |
| 3+K to 2+K+M | measure `j` | `rows` varints (Section 4.3) |

A column's raw bytes MUST hold exactly its values, with nothing after them.

## 7. Row Order and Content Rules

### 7.1. Order and uniqueness

Within a group, rows MUST be sorted by collector, then bucket, then keys in declaration order. Each is compared as a string by bytes, or as a number for bucket. Two rows MUST NOT share collector, bucket and all keys. Writers sum such rows' measures into one row.

### 7.2. Folding

When a period has more rows for one collector than the family's cap for the block's grain, the rows below the cap have their folding keys replaced with `(other)` and are summed together. Keys that don't fold keep their values, so folded rows still split by them.

With fold levels, keys of level 1 fold first. Of the rows still below the cap, level 2 folds next, and so on. Measures are never dropped, so the sum of a measure over a group equals the sum of what was written.

#### 7.2.1. Bounded folding

A family MAY name one of its measures, other than the first, as its **bound measure**. A bounded family MUST have exactly one fold level and only string or network keys. Its cap works differently, so the error it introduces is known:

1. Per collector and period, take the rows whose folding keys are not `(other)`, ranked by the first measure, largest first. If there are more than *k* (the cap), the **cut** is the first measure of the row at position *k*+1. Otherwise the cut is 0.
2. Rows whose first measure is greater than the cut keep their keys. Every other row, including rows tied with the cut, has its folding keys set to `(other)` and is summed. Ties therefore fold together, whatever their key values.
3. The **bound row** has every key, folding or not, set to `(other)`. Its first measure is 0. Its bound measure accumulates cuts: when rows of a finer period are summed into a coarser one, their bound rows are summed too, and the new period's cut is added. There is at most one bound row per collector and period. It is omitted when its bound measure would be 0 and no input carried one. When every key of the family folds, the bound row and the folded row have the same keys and are one row: its first measure is the folded sum and its bound measure the error.
4. Writers MAY put their own earlier cuts (for example, a collector that cut its data before sending it) on the bound row of each minute.

For a range of buckets, let *E* be the sum of the bound measure over the bound rows read for it, for the collector or collectors asked about. Then for every key value, its true sum lies between the sum read and that sum plus *E*. Each cut is at most 1/(*k*+1) of its period's total for that collector, so a chain of *s* cuts (for example collector, hour, day) gives *E* ≤ *s*·*N*/(*k*+1), where *N* is the range's total. Unlike the classic Misra–Gries summary, kept rows are not reduced by the cut: a kept row's count is exact for its period.

The bound row is an ordinary row in the block. A reader that doesn't know about bounded folding reads it as a folded row with a first measure of 0, which changes no sum.

### 7.3. Network keys

A network key is stored as 36 bytes of ASCII:

- 32 lowercase hex digits of the 16-byte network address, with IPv4 mapped to `::ffff:a.b.c.d`;
- `/`;
- the prefix length as 3 decimal digits, counted in IPv6 form (IPv4 lengths plus 96).

The address MUST be masked to its prefix. Because the width is fixed, byte order equals address order. All networks inside a CIDR then form one contiguous key range.

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

## 9. Reading a Block

A reader MUST take these steps and treat any failure as a damaged block. A damaged block MUST NOT be used. The store's owner SHOULD move it aside rather than delete it.

1. The file is at least 56 bytes.
2. Bytes 0–3 and the last 4 bytes are `TNB1`. Byte 4 is `1`.
3. The CRC of bytes 0–31 equals the trailer's header CRC.
4. Grain is 0, 1 or 2 and matches the directory.
5. Read the index section: the offset and length from the trailer must lie inside the file. The CRC of its stored bytes equals the trailer's index CRC. It decompresses, and parses exactly as Section 6.1.
6. For each section read later, the same three checks apply: the reference lies inside the file, the CRC matches, and the frame decompresses.
7. The dictionary's count equals `ndict`.
8. A group's `ncols` equals `2 + K + M` for its family.
9. Every column decodes to exactly `rows` values. Every index is below `ndict`, and every bucket offset is below `(end - start) / grain width`.

A reader SHOULD reject a section whose compressed length exceeds 2^31 bytes. A reader MAY decode sections lazily, only those a query needs.

## 10. Writing a Block

1. Sort and merge the rows as in Section 7.1. Build the dictionary from the collectors and key values used. Sort it, and map every value to its index.
2. Reserve 32 bytes for the header. Then write the dictionary section, each group's columns in group order, and the index section last.
3. Fill in the header. Then append the trailer with the index reference and the CRC of the finished header.
4. Write the bytes to `<name>.tmp`, flush them to stable storage (fsync), rename to `<name>`, and sync the directory.
5. When the block replaces others (a merge), only after step 4 delete the blocks it supersedes.

Given the same rows, header and compression level, a writer produces the same bytes. Tests in `internal/tblk` check this.

## 11. Versioning

The version byte is `1`. A change that older readers would misread MUST change the version byte and SHOULD change the magic. Version 1 readers MUST reject other versions. Adding a family or a measure is a schema change, not a format change. Old blocks then fail step 8 of Section 9 for that family and need rewriting.

## 12. Security Considerations

Block files are trusted local data. Readers still check them, because disks and crashes damage files:

- **The CRCs detect accidental damage, not tampering.** Anyone who can write the directory can forge a valid block.
- **Decompression can expand a small frame into a large buffer.** Readers bound compressed sizes (Section 9). They SHOULD also bound decompressed sizes, for example by expected row counts, before allocating.
- **Counts read from a file must not size allocations unchecked.** These are `rows`, `ndict`, `ngroups` and `ncols`. Readers SHOULD check them against the file size or the data actually decoded.
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
