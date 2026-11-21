# The rblk Record Block Format, Version 1

```
certainstats                                                  2026-11-20
Specification: rblk-1                                Status: Implemented
```

## Abstract

This document specifies rblk version 1. It is the on-disk format certainstats uses for records over time: rows with repeated strings, numbers, free text, identifiers and child rows, such as CrowdSec alerts and their decisions. An rblk store is a directory with a SQLite head for the newest rows and one immutable block file per UTC day. This document defines the block file byte for byte. It also defines the file names, the rule that decides which file is live, and how a partly sealed day is marked.

## Status of This Document

This document describes the format as implemented by `internal/rblk` (files `blockformat.go`, `blocks.go`, `schema.go`, and `slim.go` for identifiers). Where this document and the code disagree, the code defines the format, and this document has a bug.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conventions and Terminology](#2-conventions-and-terminology)
3. [Data Model](#3-data-model)
4. [Primitive Encodings](#4-primitive-encodings)
5. [Block File Layout](#5-block-file-layout)
6. [The Header](#6-the-header)
7. [Sections](#7-sections)
8. [Value Encodings by Kind](#8-value-encodings-by-kind)
9. [Store Directory, File Names and Sealing](#9-store-directory-file-names-and-sealing)
10. [Reading a Block](#10-reading-a-block)
11. [Writing a Block](#11-writing-a-block)
12. [Versioning](#12-versioning)
13. [Security Considerations](#13-security-considerations)
14. [References](#14-references)
- [Appendix A. Worked Example](#appendix-a-worked-example)
- [Appendix B. The CrowdSec Schema](#appendix-b-the-crowdsec-schema)
- [Appendix C. Relation to tblk](#appendix-c-relation-to-tblk)

## 1. Introduction

A record store answers lists with filters, search, sort and pages; detail by id; counts and groups; and "children still active now". rblk keeps each settled day of rows as one block, with columns stored apart:

- **Repeated strings** are kept once in a sorted dictionary per block.
- **A column equal to another column**, such as an alert's value and its IP, stores nothing.
- **A message a template can rebuild** stores nothing.
- **A UUID** takes 16 bytes.

A reader decodes only the columns a query needs. A block whose dictionary lacks a filter value is skipped without decoding its rows.

## 2. Conventions and Terminology

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in BCP 14 [RFC2119] [RFC8174] when, and only when, they appear in all capitals, as shown here.

**Owner**
: An account. Rows of every owner share a block. Each owner's rows are one contiguous range.

**Record (row)**
: An id, a time and one value per column, with zero or more child rows.

**Child**
: A row that belongs to one record, such as a decision of an alert. Children are stored flattened, in record order.

**Schema**
: The ordered column and child column declarations of Section 3, supplied by the program that uses rblk.

**Day**
: A UTC day, `[d, d + 86,400,000)` where `d` is a multiple of 86,400,000.

**Section**
: One named, zstd-compressed, CRC-checked byte range of a block file.

Times are milliseconds since 1970-01-01T00:00:00Z, as signed 64-bit integers.

## 3. Data Model

A program using rblk declares a schema:

- **Columns:** an ordered list, each with a name (`[a-z][a-z0-9_]{0,31}`) and a kind. Kinds are listed below.
- **SameAs** (optional, Dict columns only): the name of another Dict column. The column stores nothing when its value equals that column's value.
- **Templates** (optional, Text columns only): a function from a record to text. The column stores nothing when the function gives exactly the record's value.
- **Child columns:** an ordered list, with the same kinds. A child Dict column MAY have SameAs `^<name>`, naming a Dict column of its parent record. At most one child Int column is marked **Active**: its values are expiry times.
- **Dedupe column** (optional): an ID column whose value is unique per owner. It is enforced on insert, not in blocks.

| Kind | Values |
|---|---|
| Dict | Strings that repeat. Stored as dictionary codes. |
| Int | Signed 64-bit integers |
| Bool | 0 or 1, stored as Int |
| Text | Free text |
| ID | Identifiers. Canonical UUIDs take 16 bytes. |

**The schema is not stored in block files.** Section names carry column names, but not kinds, order, SameAs or templates. A reader MUST be given the same schema the writer used. A missing section is a damaged block. Changing a column's kind, SameAs or template makes old blocks unreadable or wrong, and is a format change for that store (Section 12).

SameAs resolution is recursive. A column whose SameAs column is itself SameAs-coded resolves through it. Schemas MUST NOT contain SameAs cycles.

## 4. Primitive Encodings

### 4.1. Fixed-width integers

Fixed-width integers are little-endian. `u32` is unsigned 32-bit and `u64` unsigned 64-bit.

### 4.2. uvarint

An unsigned integer is written in base-128 groups of 7 bits, least significant group first. Each byte except the last has its high bit (0x80) set. This is Protocol Buffers varints [PROTOBUF] and Go's `binary.Uvarint`. An encoding MUST NOT exceed 10 bytes, and writers MUST use the shortest encoding.

### 4.3. zigzag

A signed integer `v` maps to the unsigned `(v << 1) XOR (v >> 63)` (arithmetic shift). It is then written as a uvarint. Decoding is `(u >> 1) XOR -(u AND 1)`.

### 4.4. Delta column

A delta column of `n` signed integers `x[0..n-1]` is `n` zigzag uvarints of `x[i] - x[i-1]`, with `x[-1] = 0`. The arithmetic wraps at 64 bits. The running value continues across owners. It also continues across every record's children, in flattened order (Section 7.3).

### 4.5. CRC

CRC is CRC-32 with the IEEE 802.3 polynomial 0xEDB88320 (reflected), initial value 0xFFFFFFFF and final XOR 0xFFFFFFFF, as in zlib, gzip and PNG.

### 4.6. Compression

Every section's stored bytes are exactly one Zstandard frame [RFC8878] that decompresses to the section's raw bytes. Writers MAY use any compression level.

## 5. Block File Layout

```
+---------------------------+ offset 0
| sections ...              |   zstd frames, back to back
+---------------------------+ offset hoff
| header (JSON, hlen bytes) |
+---------------------------+ offset hoff + hlen
| trailer        (20 bytes) |
+---------------------------+ offset size = hoff + hlen + 20
```

### 5.1. Trailer

| Offset (from size-20) | Size | Field |
|---:|---:|---|
| 0 | 8 | `hoff`, the header offset, `u64` |
| 8 | 4 | `hlen`, the header length, `u32` |
| 12 | 4 | header CRC, `u32`, of the `hlen` header bytes |
| 16 | 4 | magic, ASCII `RBK1` (`52 42 4B 31`) |

`hoff + hlen + 20` MUST equal the file size. The header comes last so that the writer knows every section's place before writing it.

## 6. The Header

The header is a UTF-8 JSON object [RFC8259] with these members:

| Member | Type | Meaning |
|---|---|---|
| `v` | integer | Version, `1` |
| `start` | integer | The day's start `d` |
| `end` | integer | `d + 86400000` |
| `seq` | integer | Sequence number (Section 9.2) |
| `rows` | integer | Number of records, at least 1 |
| `children` | integer | Number of child rows, the sum of the `nchild` column |
| `max_active` | integer | The largest value of the Active child column, or 0 |
| `upto` | integer | OPTIONAL. Absent or 0 means the whole day. Otherwise `start < upto < end`, and the block holds exactly the day's rows with time before `upto` (Section 9.3). |
| `owners` | array | One object per owner (below), sorted ascending by `owner` bytes |
| `secs` | object | Section name to `{"o": offset, "n": compressed length, "c": CRC}` |

Each `owners` element is:

| Member | Type | Meaning |
|---|---|---|
| `owner` | string | The owner |
| `lo`, `hi` | integer | The owner's rows are row positions `[lo, hi)` |
| `min_id`, `max_id` | integer | Smallest and largest record id among them |

The owner ranges MUST be non-empty, MUST NOT overlap, and MUST together cover `[0, rows)` in owner order. Readers MUST ignore members they do not know. Writers SHOULD NOT add members without changing the version.

## 7. Sections

### 7.1. Names and order

| Section | Values | Encoding |
|---|---|---|
| `dict` | The dictionary | Section 7.2 |
| `id` | `rows` record ids | Delta column |
| `t` | `rows` record times | Delta column |
| `nchild` | `rows` child counts | `rows` uvarints |
| `c.<name>` | One per column | Section 8, `rows` values |
| `k.<name>` | One per child column | Section 8, `children` values |

Every section listed MUST be present. Writers store them in the order of the table, with `c.*` in column order and `k.*` in child column order. Readers MUST locate them only through `secs`. A section's raw bytes MUST hold exactly its values, with nothing after them.

### 7.2. Dictionary

```
dict = uvarint(count) count * (uvarint(len) len*byte)
```

The strings MUST be distinct and sorted ascending by bytes. They are every Dict value of every record and child that is not SameAs-coded (Section 8.1), and nothing else.

A string's **code** is its 0-based position plus 1. Code 0 is reserved for "same as" (Section 8.1). Because the dictionary is sorted, comparing codes of non-zero values compares strings. A filter value absent from the dictionary matches no row of the block.

### 7.3. Row order and children

Records MUST be sorted by owner (bytes), then time, then id. Record ids MUST be unique within the block, and SHOULD be unique within the store.

Record `i` owns `nchild[i]` children. Children are flattened in record order, so record `i`'s children are child positions `[sum(nchild[0..i-1]), sum(nchild[0..i]))`. A child's order within its record is preserved from insertion.

## 8. Value Encodings by Kind

### 8.1. Dict

Each value is one uvarint:

- **0:** the value equals the resolved value of the column's SameAs column, in the same record. For a child column with SameAs `^p`, it equals the parent record's resolved value of column `p`.
- **k ≥ 1:** the value is dictionary string `k`.

Writers MUST write 0 when, and only when, the column has SameAs and the values are equal. Code 0 in a column without SameAs is invalid. Readers SHOULD treat it as the empty string.

### 8.2. Int and Bool

Int and Bool values form a delta column. A record column's deltas run over records in row order. A child column's deltas run over children in flattened order. Bool values MUST be 0 or 1.

### 8.3. Text

In a record column with a template, each value is:

- `0x00` (uvarint 0): the template applied to the record gives the value. Writers MUST write this only when the template's output equals the value byte for byte.
- `uvarint(len + 1)` followed by `len` bytes: the value as stored.

In a child column, or a record column without a template, each value is always `uvarint(len + 1)` and the bytes.

A template MAY read any other column of the record, but not another Text column. A reader that fills only some columns MUST fill every column a template reads before applying it.

### 8.4. ID

Each value is `uvarint(len)` followed by `len` bytes of one of three forms:

| Stored bytes | Value |
|---|---|
| Exactly 16 bytes | A UUID. The value is the bytes in lowercase hex, in 8-4-4-4-12 form with hyphens. |
| `0x02 0x00` then 15 bytes | Those 15 bytes of text |
| `0x00` then the text | The text; any length but 15 |

Writers MUST use the 16-byte form for every canonical UUID: 36 characters, lowercase hex, hyphens at 8, 13, 18 and 23. Any other text, including an uppercase UUID, MUST be stored as text. The `0x02` form exists so that no text ever takes 16 bytes, which would read back as a UUID. An empty value is `0x00`.

Dedupe compares these stored bytes, so they MUST be canonical as above.

## 9. Store Directory, File Names and Sealing

```
<store>/
  head.db                       SQLite: unsealed rows and dedupe values
  blocks/<start>-<seq>.rblk     one live block per day
```

### 9.1. File names

`<start>` is the header's `start` in decimal, and `<seq>` its `seq`. Files ending in `.tmp` are unfinished writes: readers MUST ignore them, and the store's owner SHOULD delete them. A damaged block (Section 10) MUST NOT be used. The owner SHOULD rename it with a `.bad` suffix rather than delete it.

### 9.2. Which block is live

`seq` increases with every block the store writes. For each day, the block with the highest `seq` is live. Others for that day are left over from a rewrite interrupted after writing its output and before deleting its input. On open, a reader MUST ignore them, and the owner SHOULD delete them.

### 9.3. Sealing and `upto`

Rows enter `head.db` and are sealed into their day's block when settled, two hours after their time. Sealing works by the hour, so today's block is rewritten as each hour settles. A rewrite merges the head's rows with the previous block's rows, skipping ids already in the block. It is written under a new `seq` before the previous block is deleted. Then the head forgets the sealed rows, and records their dedupe values for seven days.

`upto` records how far a day is sealed. Rows of that day at or after `upto` are still in the head. After a crash between writing a block and forgetting the head's rows, the head still holds rows the block also holds. On open, the owner merges again only the head rows before the block's `upto` (or `end` when it is absent). The ids already present are skipped, so no row is lost or counted twice.

Retention cuts at a time `c`:

- days ending at or before `c` are deleted whole;
- the day containing `c` is rewritten without its rows before `c`, keeping `upto`.

Removing an owner rewrites every block that holds that owner.

The head's layout belongs to the implementation and is not part of this format.

## 10. Reading a Block

A reader MUST take these steps, and treat any failure as a damaged block:

1. The file is at least 20 bytes, and its last 4 bytes are `RBK1`.
2. `hoff + hlen + 20` equals the file size.
3. The CRC of the header bytes equals the trailer's header CRC.
4. The header parses as JSON, `v` is 1, and the owner ranges satisfy Section 6.
5. For each section it reads: the reference lies inside `[0, hoff)`, the CRC of its stored bytes matches, and the frame decompresses.
6. Each section decodes to exactly its number of values (Section 7.1). Codes are at most the dictionary size. The sum of `nchild` equals `children`.

A reader MAY decode sections lazily, only those a query needs. It MAY cache decoded sections across queries, keyed by file, because blocks never change once written.

To find record id `x` for an owner, a reader may skip blocks whose owner range has `max_id < x` or `min_id > x`. To find children active at time `now`, it may skip blocks with `max_active <= now`.

## 11. Writing a Block

1. Sort records by owner, time and id. Compute the owner ranges.
2. Collect every Dict value that is not SameAs-coded, from records and children. Sort them, and assign codes from 1.
3. Write the sections in the order of Section 7.1, recording each one's offset, compressed length and CRC.
4. Write the header JSON at the end, then the trailer.
5. Write the bytes to `<name>.tmp`, flush to stable storage (fsync), and rename to `<name>`.
6. Only after step 5, delete the day's previous block.

A block with no rows is not written. When a rewrite would leave no rows, the day's block is deleted instead.

## 12. Versioning

The header's `v` is `1`. A change that older readers would misread MUST change `v`, and SHOULD change the magic. Version 1 readers MUST reject other versions.

The schema is outside the format. Adding a column changes every block's section set, so old blocks lack the new `c.<name>` section and fail step 6. A store whose schema changes needs its blocks rewritten, or a reader that treats the missing section as a column of empty values. Version 1 readers do not.

## 13. Security Considerations

Block files are trusted local data. Readers still check them, because disks and crashes damage files:

- **CRCs detect accidental damage, not tampering.** Anyone who can write the directory can forge a valid block.
- **Decompression can expand a small frame into a large buffer.** Readers SHOULD bound decompressed sizes by the expected value counts before allocating.
- **Header counts must not size allocations unchecked.** These are `rows`, `children` and the dictionary `count`. Readers SHOULD check them against the data actually decoded.
- **Owners share a block.** A reader serving one owner MUST restrict every query to that owner's row range, and MUST NOT return another owner's rows or children.
- **Templates run on stored values.** A template MUST be a pure function of the record, so that a stored value's absence means the same thing on every read.

## 14. References

- **[RFC2119]** Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", BCP 14, RFC 2119, March 1997.
- **[RFC8174]** Leiba, B., "Ambiguity of Uppercase vs Lowercase in RFC 2119 Key Words", BCP 14, RFC 8174, May 2017.
- **[RFC8259]** Bray, T., Ed., "The JavaScript Object Notation (JSON) Data Interchange Format", STD 90, RFC 8259, December 2017.
- **[RFC8878]** Collet, Y. and M. Kucherawy, Ed., "Zstandard Compression and the 'application/zstd' Media Type", RFC 8878, February 2021.
- **[PROTOBUF]** "Protocol Buffers Encoding: Base 128 Varints", https://protobuf.dev/programming-guides/encoding/.

## Appendix A. Worked Example

The last 20 bytes of a real day block from the records study, `1790812800000-24.rblk` (1,068,173 bytes):

```
offset 1068153  da 45 10 00 00 00 00 00 9f 06 00 00 a0 0f 26 cf
offset 1068169  52 42 4b 31
```

| Bytes | Meaning |
|---|---|
| `da 45 10 00 00 00 00 00` | `hoff` = 1,066,458 |
| `9f 06 00 00` | `hlen` = 1,695 |
| `a0 0f 26 cf` | header CRC 0xCF260FA0 |
| `52 42 4b 31` | magic `RBK1` |

The header, shortened (`secs` holds every section; three are shown):

```json
{"v":1,"start":1790812800000,"end":1790899200000,"seq":24,
 "rows":20005,"children":16215,"max_active":1790985590000,
 "owners":[{"owner":"6f1d2c3b-4a5e-4f60-8b7a-9c0d1e2f3a4b",
            "lo":0,"hi":20005,"min_id":1,"max_id":20005}],
 "secs":{"c.action":{"o":622016,"n":6750,"c":3804362980},
         "c.as_name":{"o":593005,"n":29011,"c":3330559015},
         "c.collector":{"o":83291,"n":25,"c":3977299793}}}
```

The day is 2026-10-01 UTC, with 20,005 alerts and 16,215 decisions. The last decision expires at 1790985590000.

## Appendix B. The CrowdSec Schema

CrowdSec's alerts (`internal/collector/type/crowdsec/rblkstore.go`) declare these columns, in order:

- collector (Dict)
- uuid (ID, dedupe)
- lapi_id (Int)
- machine (Dict)
- scenario (Dict, indexed)
- message (Text, template)
- kind (Dict)
- ip (Dict, indexed)
- range (Dict)
- scope (Dict)
- value (Dict, SameAs ip)
- country (Dict)
- asn (Dict)
- as_name (Dict)
- action (Dict)
- events (Int)
- simulated (Bool)
- stopped (Int)
- created (Int)
- received (Int)

Its children are decisions:

- uuid (ID)
- type (Dict)
- origin (Dict)
- scope (Dict)
- value (Dict, SameAs `^value`)
- duration (Dict)
- until (Int, Active)

The message template is `"<scope> <value> performed '<scenario>' (<events> events over <stopped - time>)"`, with the duration written in Go's `time.Duration` form, e.g. `2s` or `1m30s`.

## Appendix C. Relation to tblk

tblk ([tblk.md](tblk.md)) shares the zstd sections, CRC-32, the per-block sorted dictionary and the `.tmp`-then-rename write. It differs in four ways:

- a binary header at the front and a binary index section;
- 0-based dictionary indexes;
- run-length collector columns;
- one block per (grain, span) holding counts rather than records.
