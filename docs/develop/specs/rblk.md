# The rblk Record Block Format, Version 1

```
certainstats                                                  2026-11-21
Specification: rblk-1, revision 2                    Status: Implemented
```

## Abstract

This document specifies rblk version 1. It is the on-disk format certainstats uses for records over time: rows with repeated strings, numbers, free text, identifiers and child rows, such as CrowdSec alerts and their decisions. An rblk store is a directory with a SQLite head for the newest rows and one immutable block file per UTC day. This document defines the block file byte for byte. It also defines the file names, the rule that decides which file is live, and how a partly sealed day is marked.

## Status of This Document

This document describes the format as implemented by `internal/rblk` (files `blockformat.go`, `blocks.go`, `schema.go`, and `slim.go` for identifiers). Where this document and the code disagree, the code defines the format, and this document has a bug.

**Revision 2** changes no byte of the format: every block a correct revision 1 writer produced is still valid, and all 147 blocks of the records study pass the new checks. It makes the reader rules complete and normative (Section 10): header ranges, section placement, one zstd frame per section, size limits, and values that revision 1 let readers accept or read as empty (code 0 without SameAs, Text code 0 without a template, malformed IDs, child counts that do not sum). It defines the store's one-writer rule and lock (Section 9.4), the exact bytes of the schema fingerprint, the record time range of a block, delta arithmetic as two's complement, the JSON rules of the header, the durability order of a rewrite including the directory sync, the head's transaction boundaries, and which schema changes a store accepts.

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

To catch a mismatched schema, certainstats records a **shape** fingerprint of the schema on a store's first open, and refuses to open the store with a schema of another shape (`ErrShapeChanged`). It is kept in the head (`head.db`, or `records.db` for the Slim engine), table `meta`, key `shape`, as the hash's 64 bits read as a signed integer. A store from before shapes were recorded takes the schema of its first open after the upgrade on trust.

The shape is 64-bit FNV-1a (offset basis 0xcbf29ce484222325, prime 0x100000001b3) over this byte sequence:

```
field(s)   = the bytes of s, then 0x00
group(...) = field of each part in order, then 0x01

for each column, in order:
    group("c", name, kind, same_as, template)
for each child column, in order:
    group("k", name, kind, same_as, active)
group("d", dedupe)
```

- `kind` is the decimal digit of the kind: `0` Dict, `1` Int, `2` Bool, `3` Text, `4` ID.
- `same_as` is the SameAs text as declared (`^p` for a child's parent column), or the empty string when absent.
- `template` and `active` are `true` or `false`.
- `dedupe` is the dedupe column's name, or the empty string when there is none.

The column count is not hashed separately: each column is its own group, so a different count gives a different sequence. Names follow the grammar above, so 0x00 and 0x01 cannot occur inside a field. Indexes are not part of the shape. FNV-1a catches accidental mismatches only. It is not collision-resistant, and the shape is no defence against a schema built to collide.

SameAs resolution is recursive. A column whose SameAs column is itself SameAs-coded resolves through it. Schemas MUST NOT contain SameAs cycles, a column that is SameAs itself included; certainstats refuses such a schema. Child column names MUST be distinct, like column names.

## 4. Primitive Encodings

### 4.1. Fixed-width integers

Fixed-width integers are little-endian. `u32` is unsigned 32-bit and `u64` unsigned 64-bit.

### 4.2. uvarint

An unsigned integer is written in base-128 groups of 7 bits, least significant group first. Each byte except the last has its high bit (0x80) set. This is Protocol Buffers varints [PROTOBUF] and Go's `binary.Uvarint`. An encoding MUST NOT exceed 10 bytes, and writers MUST use the shortest encoding. Readers MUST reject an encoding that does not end within 10 bytes, one cut off by the end of its section, and one whose tenth byte has any bit other than bit 0 set (it would overflow 64 bits). Readers MAY accept encodings that are longer than the shortest; certainstats does.

### 4.3. zigzag

A signed integer `v` maps to the unsigned `(v << 1) XOR (v >> 63)` (arithmetic shift). It is then written as a uvarint. Decoding is `(u >> 1) XOR -(u AND 1)`.

### 4.4. Delta column

A delta column of `n` signed integers `x[0..n-1]` is `n` zigzag uvarints of `d[i] = x[i] - x[i-1]`, with `x[-1] = 0`. The arithmetic is two's complement on 64 bits: take the values' 64-bit patterns as unsigned, subtract modulo 2^64, and read the result as a signed 64-bit `d[i]` for zigzag. A reader adds the decoded `d[i]` to the running value the same way, modulo 2^64, and reads the sum as signed. So `x[0] = 2^63-1` followed by `x[1] = -2^63` stores `d[1] = 1`. Implementations in languages where signed overflow is undefined or trapping MUST do this arithmetic on unsigned values. The running value continues across owners. It also continues across every record's children, in flattened order (Section 7.3).

### 4.5. CRC

CRC is CRC-32 with the IEEE 802.3 polynomial 0xEDB88320 (reflected), initial value 0xFFFFFFFF and final XOR 0xFFFFFFFF, as in zlib, gzip and PNG.

### 4.6. Compression

Every section's stored bytes are exactly one Zstandard frame [RFC8878] that decompresses to the section's raw bytes: the frame header, its blocks up to the one marked last, and the 4-byte checksum if the header declares one, with no other bytes and no skippable frames. Writers MAY use any compression level, but the frame's window (its Window_Size, RFC 8878 section 3.1.1.1.2) MUST be at most 2^27 bytes (128 MiB). certainstats writes windows of 8 MiB or less.

Frames MUST NOT use a Zstandard compression dictionary (RFC 8878 section 5); rblk has no way to name or ship one. This is unrelated to the string dictionary of Section 7.2. A frame's header MUST NOT carry a Dictionary_ID other than 0, and every frame MUST decode with no dictionary supplied. A reader decodes without a dictionary and treats a frame that declares or needs one as damaged.

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

`hoff + hlen + 20` MUST equal the file size, and `hlen` MUST be between 2 and 2^26 (64 MiB), so a reader can check it before allocating the header. The header comes last so that the writer knows every section's place before writing it.

## 6. The Header

The header is a UTF-8 JSON object [RFC8259] with these members:

| Member | Type | Meaning |
|---|---|---|
| `v` | integer | Version, `1` |
| `start` | integer | The day's start `d`, a multiple of 86,400,000, from 0 to 2^63-1-86,400,000 |
| `end` | integer | Exactly `start + 86400000` |
| `seq` | integer | Sequence number (Section 9.2), at least 1 |
| `rows` | integer | Number of records, from 1 to 2^31 |
| `children` | integer | Number of child rows, the sum of the `nchild` column, from 0 to 2^31 |
| `max_active` | integer | The largest value of the Active child column, or 0 |
| `upto` | integer | OPTIONAL. Absent or 0 means the whole day. Otherwise `start < upto < end`, and the block holds exactly the day's rows with time before `upto` (Section 9.3). |
| `owners` | array | One object per owner (below), sorted ascending by `owner` bytes |
| `secs` | object | Section name to `{"o": offset, "n": compressed length, "c": CRC}` |

Each `owners` element is:

| Member | Type | Meaning |
|---|---|---|
| `owner` | string | The owner, a non-empty string |
| `lo`, `hi` | integer | The owner's rows are row positions `[lo, hi)` |
| `min_id`, `max_id` | integer | Smallest and largest record id among them, `min_id ≤ max_id` |

The owner ranges MUST be non-empty, MUST NOT overlap, and MUST together cover `[0, rows)` in owner order. Owners MUST be distinct, so the array is strictly ascending. An owner is compared and sorted as the UTF-8 bytes of its string after JSON escape processing, in the unsigned byte order of Section 7.2.

The header's JSON follows these rules, and a header that breaks one is damaged:

- The header bytes are valid UTF-8. Readers MUST reject invalid UTF-8 rather than replace malformed sequences, since owner and section names are compared by bytes.
- No string holds a `\u` escape of a lone UTF-16 surrogate: a `\uD800`–`\uDBFF` escape MUST be followed at once by a `\uDC00`–`\uDFFF` escape, and a low surrogate escape MUST NOT appear on its own. Readers MUST reject such a header rather than decode the escape as U+FFFD. Other escapes are allowed.
- No object, at any depth, repeats a member name. Names are compared after JSON escape processing, so `"\u0072ows"` repeats `"rows"`; readers MUST compare decoded names, not their raw bytes. (JSON parsers disagree on which copy wins, so a repeated name is rejected rather than resolved.)
- In an object whose members this section defines (the header itself, each `owners` element, and each `secs` reference), a member name that is not exactly a defined name but matches one under Unicode case folding, as Go's `strings.EqualFold` and `encoding/json` field matching do, MUST be rejected. Examples: `"Rows"` or `"ſeq"` (with U+017F) in the header, `"OWNER"` in an owner, `"O"` in a section reference. Go's decoder would read such a name as the defined member instead of ignoring it. The rule applies to each of those objects separately, and not to the `secs` object's own names (section names, compared exactly) or to anything inside an unknown member, where `"Rows"` is plain data.
- Every member this document types as integer is a JSON number with no fraction and no exponent, within the signed 64-bit range. The rule covers only those members.
- Readers MUST ignore members they do not know, at any depth, whatever valid JSON value they hold, including numbers with fractions or exponents. Writers SHOULD NOT add members without changing the version.
- In `secs`, each reference's `o`, `n` and `c` are integers, under the integer rule above: `o` from 0 to `hoff - n`, `n` from 1 to 2^31 (Section 7.1), and `c` from 0 to 2^32-1.

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

Every section listed MUST be present. Writers store them in the order of the table, with `c.*` in column order and `k.*` in child column order, back to back from offset 0. Readers MUST locate them only through `secs`. A section's raw bytes MUST hold exactly its values, with nothing after them.

Every reference in `secs`, including any name the reader does not know, MUST lie inside `[0, hoff)` with a length of at least 1 and at most 2^31, and no two references may overlap. A reader ignores sections it does not know, but still checks their references. The JSON rules of Section 6 rule out a name listed twice.

### 7.2. Dictionary

```
dict = uvarint(count) count * (uvarint(len) len*byte)
```

The strings MUST be distinct and sorted ascending by bytes, with nothing after the last. "By bytes" means lexicographic order of unsigned byte values: the first differing byte decides, and a string that is a prefix of another sorts first. It is not locale-aware, and bytes are not compared as signed values. They are every Dict value of every record and child that is not SameAs-coded (Section 8.1), and nothing else. The format sets no limit on the count or on a string's length beyond the section's 2^31-byte limit. Since every string takes at least its length byte, `count` MUST NOT exceed the bytes that follow it, and a reader checks this before allocating.

A string's **code** is its 0-based position plus 1. Code 0 is reserved for "same as" (Section 8.1). Because the dictionary is sorted, comparing codes of non-zero values compares strings. A filter value absent from the dictionary matches no row of the block.

### 7.3. Row order and children

Records MUST be sorted by owner (bytes, as in Section 7.2), then time, then id. Record ids MUST be unique within the block, and SHOULD be unique within the store.

Every record's time `t` MUST lie in the block's day, `start ≤ t < end`, and, when `upto` is present and not 0, before it: `t < upto`. Readers skip blocks by their day and by `upto` (Section 9.3), so a record outside them can be missed or counted twice.

Record `i` owns `nchild[i]` children. Children are flattened in record order, so record `i`'s children are child positions `[sum(nchild[0..i-1]), sum(nchild[0..i]))`. A child's order within its record is preserved from insertion.

## 8. Value Encodings by Kind

### 8.1. Dict

Each value is one uvarint:

- **0:** the value equals the resolved value of the column's SameAs column, in the same record. For a child column with SameAs `^p`, it equals the parent record's resolved value of column `p`.
- **k ≥ 1:** the value is dictionary string `k`.

Writers MUST write 0 when, and only when, the column has SameAs and the values are equal. Code 0 in a column without SameAs, or a code greater than the dictionary's count, makes the block damaged. (Revision 1 let readers read code 0 there as the empty string. No correct writer produced it, and reading damage as a value would hide it.)

### 8.2. Int and Bool

Int and Bool values form a delta column. A record column's deltas run over records in row order. A child column's deltas run over children in flattened order. Bool values MUST be 0 or 1.

### 8.3. Text

In a record column with a template, each value is:

- `0x00` (uvarint 0): the template applied to the record gives the value. Writers MUST write this only when the template's output equals the value byte for byte.
- `uvarint(len + 1)` followed by `len` bytes: the value as stored.

In a child column, or a record column without a template, each value is always `uvarint(len + 1)` and the bytes; a value of 0 there makes the block damaged.

A template MAY read any other column of the record, but not another Text column. A reader that fills only some columns MUST fill every column a template reads before applying it.

### 8.4. ID

Each value is `uvarint(len)` followed by `len` bytes of one of three forms:

| Stored bytes | Value |
|---|---|
| Exactly 16 bytes | A UUID. The value is the bytes in lowercase hex, in 8-4-4-4-12 form with hyphens. |
| `0x02 0x00` then 15 bytes | Those 15 bytes of text |
| `0x00` then the text | The text; any length but 15 |

Writers MUST use the 16-byte form for every canonical UUID: 36 characters, lowercase hex, hyphens at 8, 13, 18 and 23. Any other text, including an uppercase UUID, MUST be stored as text. The `0x02` form exists so that no text ever takes 16 bytes, which would read back as a UUID. An empty value is `0x00`.

Any other stored bytes make the block damaged: a length of 0, a first byte other than `0x00` or `0x02` in a value that is not 16 bytes long, or a `0x02` value that is not exactly `0x02 0x00` and 15 bytes. The 16-byte form takes any bytes; every 16-byte value is a UUID.

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

`seq` increases with every block the store writes. A writer MUST NOT wrap it: when the store's highest `seq` is 2^63-1, writing stops with an error, since a wrapped number would lose to every older block. For each day, the block with the highest `seq` is live. Others for that day are left over from a rewrite interrupted after writing its output and before deleting its input. On open, a reader MUST ignore them, and the owner SHOULD delete them.

### 9.3. Sealing and `upto`

Rows enter `head.db` and are sealed into their day's block when settled, two hours after their time. Sealing works by the hour, so today's block is rewritten as each hour settles. A rewrite merges the head's rows with the previous block's rows, skipping ids already in the block. It is written under a new `seq` before the previous block is deleted. Then the head forgets the sealed rows, and records their dedupe values for seven days.

The steps and their order are what make a crash safe, provided the file system and the storage device honour `fsync` (Section 11). A device that acknowledges writes it has only cached voids these guarantees, as it does SQLite's.

1. Write the new block and make it durable (Section 11, steps 5 and 6), including the directory entry.
2. Delete the previous block. This does not need to be durable: if the deletion is lost, both files exist and the higher `seq` wins (Section 9.2).
3. In one SQLite transaction, record the sealed rows' dedupe values and delete the rows and their children from the head. If a crash loses the transaction, the head still holds the rows, and the next open merges them again (below).

Inserts are one SQLite transaction per batch: a batch's rows, their children, and its dedupe check commit together or not at all. The head runs in WAL mode with `synchronous=NORMAL`, so a power cut can lose the last committed transactions but not corrupt the head. A lost insert loses those rows unless their source sends them again; rblk does not resend. A lost step 3 leaves rows the block already holds, which the merge on open skips.

`upto` records how far a day is sealed. Rows of that day at or after `upto` are still in the head. After a crash between writing a block and forgetting the head's rows, the head still holds rows the block also holds. On open, the owner merges again only the head rows before the block's `upto` (or `end` when it is absent). The ids already present are skipped, so no row is lost or counted twice.

Retention cuts at a time `c`:

- days ending at or before `c` are deleted whole;
- the day containing `c` is rewritten without its rows before `c`, keeping `upto`.

Removing owners is part of the same operation as retention, and is stated as the owners to keep: every other owner's rows go. It deletes their head rows, their children and their dedupe values, and rewrites every block that holds them (or deletes the block when no rows remain).

Retention and owner removal are idempotent rather than atomic: each block is rewritten or deleted on its own, so a crash part way leaves some blocks done and others not. Running the operation again with the same cut and owners finishes it. certainstats runs it in every hourly maintenance with the current accounts, so an interrupted removal completes within the hour after the restart. Until then, a removed owner's rows can remain in blocks not yet rewritten.

A block deleted with no replacement (a whole day past the cut, or a rewrite that would keep no rows) has no higher `seq` to hide it if its directory entry comes back after a power cut. The writer MUST sync the `blocks` directory after such deletions and before the operation reports success; one sync after all deletions of a pass is enough. Deleting a block that a durable higher-`seq` block replaced needs no sync (step 2 above).

The head's layout belongs to the implementation and is not part of this format.

### 9.4. One writer

A store has one writing process at a time. It covers everything that changes the store: recovery on open (Section 9.3), inserts, sealing, retention, owner removal and the choice of `seq`. SQLite makes each head transaction atomic, but a rewrite spans a file and a transaction, and two writers could take the same `seq`, write different blocks for one day, and forget each other's rows.

certainstats enforces this with an exclusive, non-blocking `flock` on `<store>/lock`, taken when the store opens and held until it closes. A second open fails with `ErrLocked`. The operating system drops the lock when the process ends, so a crash leaves none behind. The lock file itself stays: close and cleanup MUST NOT remove it, since a process that deleted and recreated it while another held the lock would lock a different file at the same path, and both would write. Every open takes the lock; there is no read-only open, so while one process has the store, other processes cannot open it at all. On systems without `flock`, one process per store is the program's job. Within the process that owns the store, any number of queries may run at once. Blocks never change once written, and the head is read through SQLite's WAL.

## 10. Reading a Block

A reader MUST take these steps, and treat any failure as a damaged block:

1. The file is at least 20 bytes, and its last 4 bytes are `RBK1`.
2. `hlen` is between 2 and 2^26, and `hoff + hlen + 20` equals the file size, computed without overflow.
3. The CRC of the header bytes equals the trailer's header CRC.
4. The header parses as JSON under the rules of Section 6. `v` is 1. `start` is a multiple of 86,400,000 from 0 to 2^63-1-86,400,000, so that `start + 86400000` cannot overflow, and `end` equals it. `seq`, `rows`, `children` and `upto` are in their ranges. The owner ranges satisfy Section 6, and no owner is empty.
5. The file name is `<start>-<seq>.rblk` for the header's values.
6. `secs` holds every section Section 7.1 requires for the schema. Every reference satisfies Section 7.1: inside `[0, hoff)`, length 1 to 2^31, none overlapping.
7. For each section it reads: the CRC of its stored bytes matches, the stored bytes are exactly one zstd frame (Section 4.6), the frame's window is at most 2^27 bytes, and the frame decompresses to at most 2^31 bytes.
8. Each section decodes to exactly its number of values (Section 7.1), with nothing after them. Before allocating for `n` values, the reader checks that the raw bytes are at least `n` long, since every value takes at least one byte. The dictionary is sorted and distinct (Section 7.2). Dict codes are at most the dictionary's count, and 0 only where Section 8.1 allows it. `nchild` sums to `children`, checked so the sum cannot overflow: before adding each value `v`, the reader requires `v ≤ children - sum`, where `sum` is the total so far, and the final sum must equal `children`. Child positions are built from these checked counts only. Text and ID values have the forms of Sections 8.3 and 8.4.

Steps 1 to 6 need only the header and run when a store opens; certainstats renames a block that fails them to `.bad`. Steps 7 and 8 run when a section is first read, and a failure there fails the query.

These checks are for structural safety: a block that passes them cannot make a reader read out of range or crash on a bad count, and every allocation is bounded by the limits above. Those limits are large, so a reader on a small machine can still run out of memory on a block that passes. Full conformance asks more, and the reader does not check it: rows sorted and ids unique, and every time inside the day and before `upto` (Section 7.3), the dictionary holding exactly the values the columns use (Section 7.2; an unused string wastes space but is never read), Bool values 0 or 1, the shortest uvarint encodings, `max_active`, `min_id` and `max_id` matching the rows, and templated values that the template really gives. A block that breaks these still decodes safely, but can give wrong answers for its own rows.

A reader MAY decode sections lazily, only those a query needs. It MAY cache decoded sections across queries, keyed by file, because blocks never change once written.

To find record id `x` for an owner, a reader may skip blocks whose owner range has `max_id < x` or `min_id > x`. To find children active at time `now`, it may skip blocks with `max_active <= now`.

## 11. Writing a Block

1. Sort records by owner, time and id. Compute the owner ranges.
2. Collect every Dict value that is not SameAs-coded, from records and children. Sort them, and assign codes from 1.
3. Write the sections in the order of Section 7.1, recording each one's offset, compressed length and CRC.
4. Write the header JSON at the end, then the trailer.
5. Write the bytes to `<name>.tmp`, flush the file to stable storage (fsync), and rename it to `<name>`.
6. Flush the directory (fsync on the `blocks` directory), so that the rename survives a power cut.
7. Only after step 6, delete the day's previous block, and only then let the head forget the sealed rows (Section 9.3). Without step 6, a power cut could keep the head's deletion but lose the new block's name.

A block with no rows is not written. When a rewrite would leave no rows, the day's block is deleted instead, and the deletion is made durable with a directory sync (Section 9.3).

## 12. Versioning

The header's `v` is `1`. A change that older readers would misread MUST change `v`, and SHOULD change the magic. Version 1 readers MUST reject other versions.

The schema is outside the format, but a store refuses a schema whose shape changed (Section 3). The schema is versioned with the program, and these rules decide what a change needs:

| Change | Effect |
|---|---|
| Add or remove an index | Accepted. Indexes change no stored byte. |
| Add, remove, rename or reorder a column or child column | Refused (`ErrShapeChanged`). Old blocks lack the new section or hold the old one. |
| Change a kind, a SameAs, the Active mark or the dedupe column | Refused. Old values would read as another kind or resolve differently. |
| Add or remove a template | Refused. Code 0 would change meaning. |
| Change what a template outputs | Not detected. Old rows stored with code 0 would read back as the new text. A program MUST NOT change a template's output for values already stored; it adds a new Text column, or rewrites the store's blocks, instead. |

A store whose schema must change gets a new store directory, or has its blocks rewritten by a program that reads with the old schema and writes with the new one.

## 13. Security Considerations

Block files are trusted local data. Readers still check them, because disks and crashes damage files:

- **CRCs detect accidental damage, not tampering.** Anyone who can write the directory can forge a valid block.
- **Decompression can expand a small frame into a large buffer.** Readers MUST bound every section's decompressed size before allocating it. certainstats sets the limit to 2^31 bytes in the zstd decoder.
- **A frame's window sizes the decoder's history buffer**, whatever the output size, so a small frame could declare a huge one. Readers MUST bound it separately. certainstats rejects windows over 2^27 bytes.
- **Header counts must not size allocations unchecked.** These are `hlen`, `rows`, `children` and the dictionary `count`. Readers MUST check them against their limits and against the raw bytes available (Section 10, steps 2, 4 and 8) before allocating.
- **Child positions come from a prefix sum.** The reader checks that `nchild` sums to `children` before using it, so no child position falls outside the child columns. certainstats keeps the prefix sum per block in memory, so a record's children are found without rescanning.
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
