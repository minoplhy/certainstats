package tblk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Block file layout (version 1), all integers little-endian:
//
//	header   32 bytes: magic "TNB1", version, grain, 2 spare, start, end, covered (int64 ms)
//	sections zstd-compressed, each checked by CRC32 of its compressed bytes:
//	         dictionary: count, then length-prefixed strings in sorted order
//	         per (owner, family) group, one section per column:
//	           collector  run-length pairs (dictionary index, run)
//	           bucket     offset from start in grain units
//	           each key   dictionary index
//	           each measure  signed varint
//	         index: dictionary ref, then per group owner, family, rows, folded
//	                rows and its column refs
//	trailer  24 bytes: index offset (8), index length (4), index CRC (4),
//	         header CRC (4), magic "TNB1"
//
// Rows in a group are sorted by collector, bucket, keys. A block stands alone:
// its own dictionary, so retention deletes files and needs no global sweep.
// Because the dictionary is sorted and network keys sort in address order, a
// CIDR filter is a range of dictionary indexes.

const (
	blockMagic   = "TNB1"
	blockVersion = 1
	headerLen    = 32
	trailerLen   = 24
)

var errBadBlock = errors.New("tblk: block failed its checks")

// maxSection bounds a section's compressed and decoded size, so a damaged
// or hostile frame cannot size an allocation.
const maxSection = 1 << 31

type blockHeader struct {
	grain               Grain
	start, end, covered int64
}

type secRef struct {
	off, n int64
	crc    uint32
}

type groupRef struct {
	owner, family string
	rows, folded  int64
	cols          []secRef // collector, bucket, keys…, measures…
}

type blockFile struct {
	path  string
	h     blockHeader
	size  int64
	dict  secRef
	ndict int
	group []groupRef
	// From the file name, set by the engine: seq orders writes; a merged
	// block replaces every lower-seq block inside its range.
	seq    int64
	merged bool
}

func (b *blockFile) find(owner, family string) *groupRef {
	for i := range b.group {
		if b.group[i].owner == owner && b.group[i].family == family {
			return &b.group[i]
		}
	}
	return nil
}

func (b *blockFile) rows() (n, folded int64) {
	for _, g := range b.group {
		n += g.rows
		folded += g.folded
	}
	return n, folded
}

// codec holds the shared zstd encoder and decoder.
type codec struct {
	enc *zstd.Encoder
	dec *zstd.Decoder
}

func newCodec() (*codec, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression), zstd.WithEncoderConcurrency(1), zstd.WithLowerEncoderMem(true))
	if err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(4), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxMemory(maxSection))
	if err != nil {
		enc.Close()
		return nil, err
	}
	return &codec{enc: enc, dec: dec}, nil
}

func (c *codec) close() {
	c.enc.Close()
	c.dec.Close()
}

// blockRows are a block's rows to write, per (owner, family).
type blockRows map[[2]string][]rec

// sortMerge sorts rows by collector, bucket and keys, and sums rows that share
// all three. It works in place and returns the shortened list; values shared
// with a caller are copied before they are summed into.
func sortMerge(list []rec) []rec {
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.collector != b.collector {
			return a.collector < b.collector
		}
		if a.bucket != b.bucket {
			return a.bucket < b.bucket
		}
		return lessKeys(a.keys, b.keys)
	})
	if len(list) < 2 {
		return list
	}
	w, owned := 0, false
	for i := 1; i < len(list); i++ {
		a, b := &list[w], list[i]
		if a.collector == b.collector && a.bucket == b.bucket && equalKeys(a.keys, b.keys) {
			if !owned {
				a.vals, owned = append([]int64(nil), a.vals...), true
			}
			for j, v := range b.vals {
				a.vals[j] = satAdd(a.vals[j], v)
			}
			continue
		}
		w, owned = w+1, false
		list[w] = b
	}
	return list[:w+1]
}

// pendGroup is one (owner, family) group turned into compact columns: key
// strings are provisional dictionary ids until finish sorts the dictionary.
type pendGroup struct {
	owner, family string
	f             *famInfo
	folded        int64
	lastCol       string
	col, bucket   []uint32
	keys          [][]uint32
	vals          [][]int64
}

// blockWriter builds a block one group (or one collector of a group) at a
// time, so a caller never holds more than that as rows. finish writes the
// same bytes writeBlock always has: sorted dictionary, groups by owner and
// family, rows by collector, bucket and keys.
type blockWriter struct {
	// seq and merged name the file; the engine copies them to the blockFile.
	seq    int64
	merged bool
	c      *codec
	si     *schemaInfo
	path   string
	h      blockHeader
	ids    map[string]uint32
	strs   []string
	groups map[[2]string]*pendGroup
}

func newBlockWriter(c *codec, si *schemaInfo, path string, h blockHeader) *blockWriter {
	return &blockWriter{c: c, si: si, path: path, h: h, ids: map[string]uint32{}, groups: map[[2]string]*pendGroup{}}
}

func (w *blockWriter) empty() bool { return len(w.groups) == 0 }

func (w *blockWriter) intern(s string) uint32 {
	if id, ok := w.ids[s]; ok {
		return id
	}
	id := uint32(len(w.strs))
	w.ids[s] = id
	w.strs = append(w.strs, s)
	return id
}

// add appends rows to the (owner, family) group. A group may be added in
// several calls when each call's collectors all sort after the previous
// call's. rows is sorted in place and may be dropped afterwards.
func (w *blockWriter) add(owner, family string, rows []rec) error {
	if len(rows) == 0 {
		return nil
	}
	f := w.si.byName[family]
	if f == nil {
		return fmt.Errorf("tblk: block rows for unknown family %q", family)
	}
	rows = sortMerge(rows)
	gk := [2]string{owner, family}
	g := w.groups[gk]
	if g == nil {
		g = &pendGroup{owner: owner, family: family, f: f, keys: make([][]uint32, len(f.Keys)), vals: make([][]int64, len(f.Measures))}
		w.groups[gk] = g
	} else if rows[0].collector <= g.lastCol {
		return fmt.Errorf("tblk: rows for %s/%s collector %q added after %q", owner, family, rows[0].collector, g.lastCol)
	}
	gms := w.h.grain.Ms()
	for _, r := range rows {
		if r.bucket < w.h.start || r.bucket >= w.h.end {
			return fmt.Errorf("tblk: bucket %d outside block %d-%d", r.bucket, w.h.start, w.h.end)
		}
		g.col = append(g.col, w.intern(r.collector))
		g.bucket = append(g.bucket, uint32((r.bucket-w.h.start)/gms))
		for k := range g.keys {
			g.keys[k] = append(g.keys[k], w.intern(r.keys[k]))
		}
		for m := range g.vals {
			g.vals[m] = append(g.vals[m], r.vals[m])
		}
		if f.folded(r.keys) {
			g.folded++
		}
	}
	g.lastCol = rows[len(rows)-1].collector
	return nil
}

// finish writes the block atomically: a .tmp file, fsync, rename.
func (w *blockWriter) finish() (*blockFile, error) {
	c, h, path := w.c, w.h, w.path
	order := make([]uint32, len(w.strs))
	for i := range order {
		order[i] = uint32(i)
	}
	sort.Slice(order, func(i, j int) bool { return w.strs[order[i]] < w.strs[order[j]] })
	remap := make([]uint64, len(w.strs))
	for sorted, id := range order {
		remap[id] = uint64(sorted)
	}

	var buf bytes.Buffer
	buf.Write(make([]byte, headerLen))
	section := func(raw []byte) secRef {
		comp := c.enc.EncodeAll(raw, nil)
		ref := secRef{off: int64(buf.Len()), n: int64(len(comp)), crc: crc32.ChecksumIEEE(comp)}
		buf.Write(comp)
		return ref
	}
	var raw []byte
	raw = binary.AppendUvarint(raw, uint64(len(order)))
	for _, id := range order {
		s := w.strs[id]
		raw = binary.AppendUvarint(raw, uint64(len(s)))
		raw = append(raw, s...)
	}
	bf := &blockFile{path: path, h: h, ndict: len(order)}
	bf.dict = section(raw)

	gkeys := make([][2]string, 0, len(w.groups))
	for k := range w.groups {
		gkeys = append(gkeys, k)
	}
	sort.Slice(gkeys, func(i, j int) bool {
		if gkeys[i][0] != gkeys[j][0] {
			return gkeys[i][0] < gkeys[j][0]
		}
		return gkeys[i][1] < gkeys[j][1]
	})
	var col []byte
	for _, gk := range gkeys {
		pg := w.groups[gk]
		n := len(pg.col)
		g := groupRef{owner: pg.owner, family: pg.family, rows: int64(n), folded: pg.folded}
		// collector: run-length pairs
		col = col[:0]
		for i := 0; i < n; {
			j := i
			for j < n && pg.col[j] == pg.col[i] {
				j++
			}
			col = binary.AppendUvarint(col, remap[pg.col[i]])
			col = binary.AppendUvarint(col, uint64(j-i))
			i = j
		}
		g.cols = append(g.cols, section(col))
		col = col[:0]
		for _, b := range pg.bucket {
			col = binary.AppendUvarint(col, uint64(b))
		}
		g.cols = append(g.cols, section(col))
		for _, kc := range pg.keys {
			col = col[:0]
			for _, id := range kc {
				col = binary.AppendUvarint(col, remap[id])
			}
			g.cols = append(g.cols, section(col))
		}
		for _, vc := range pg.vals {
			col = col[:0]
			for _, v := range vc {
				col = binary.AppendVarint(col, v)
			}
			g.cols = append(g.cols, section(col))
		}
		bf.group = append(bf.group, g)
		delete(w.groups, gk) // its columns are written; let them go
	}

	// index
	raw = raw[:0]
	putRef := func(r secRef) {
		raw = binary.AppendUvarint(raw, uint64(r.off))
		raw = binary.AppendUvarint(raw, uint64(r.n))
		raw = binary.LittleEndian.AppendUint32(raw, r.crc)
	}
	putStr := func(s string) {
		raw = binary.AppendUvarint(raw, uint64(len(s)))
		raw = append(raw, s...)
	}
	putRef(bf.dict)
	raw = binary.AppendUvarint(raw, uint64(bf.ndict))
	raw = binary.AppendUvarint(raw, uint64(len(bf.group)))
	for _, g := range bf.group {
		putStr(g.owner)
		putStr(g.family)
		raw = binary.AppendUvarint(raw, uint64(g.rows))
		raw = binary.AppendUvarint(raw, uint64(g.folded))
		raw = binary.AppendUvarint(raw, uint64(len(g.cols)))
		for _, r := range g.cols {
			putRef(r)
		}
	}
	ix := section(raw)

	out := buf.Bytes()
	copy(out[0:4], blockMagic)
	out[4], out[5] = blockVersion, byte(h.grain)
	binary.LittleEndian.PutUint64(out[8:], uint64(h.start))
	binary.LittleEndian.PutUint64(out[16:], uint64(h.end))
	binary.LittleEndian.PutUint64(out[24:], uint64(h.covered))
	var tr [trailerLen]byte
	binary.LittleEndian.PutUint64(tr[0:], uint64(ix.off))
	binary.LittleEndian.PutUint32(tr[8:], uint32(ix.n))
	binary.LittleEndian.PutUint32(tr[12:], ix.crc)
	binary.LittleEndian.PutUint32(tr[16:], crc32.ChecksumIEEE(out[:headerLen]))
	copy(tr[20:], blockMagic)
	out = append(out, tr[:]...)

	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	fh, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	if _, err := fh.Write(out); err != nil {
		fh.Close()
		os.Remove(tmp)
		return nil, err
	}
	if err := fh.Sync(); err != nil {
		fh.Close()
		os.Remove(tmp)
		return nil, err
	}
	if err := fh.Close(); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	bf.size = int64(len(out))
	return bf, nil
}

// writeBlock writes a whole block from rows grouped by (owner, family).
func writeBlock(c *codec, si *schemaInfo, path string, h blockHeader, rows blockRows) (*blockFile, error) {
	w := newBlockWriter(c, si, path, h)
	for gk, list := range rows {
		if err := w.add(gk[0], gk[1], list); err != nil {
			return nil, err
		}
	}
	return w.finish()
}

// openBlock reads and checks a block's header, trailer and index.
func openBlock(c *codec, path string) (*blockFile, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < headerLen+trailerLen {
		return nil, errBadBlock
	}
	var hd [headerLen]byte
	var tr [trailerLen]byte
	if _, err := fh.ReadAt(hd[:], 0); err != nil {
		return nil, err
	}
	if _, err := fh.ReadAt(tr[:], st.Size()-trailerLen); err != nil {
		return nil, err
	}
	if string(hd[0:4]) != blockMagic || string(tr[20:24]) != blockMagic || hd[4] != blockVersion ||
		crc32.ChecksumIEEE(hd[:]) != binary.LittleEndian.Uint32(tr[16:]) {
		return nil, errBadBlock
	}
	bf := &blockFile{path: path, size: st.Size(), h: blockHeader{
		grain:   Grain(hd[5]),
		start:   int64(binary.LittleEndian.Uint64(hd[8:])),
		end:     int64(binary.LittleEndian.Uint64(hd[16:])),
		covered: int64(binary.LittleEndian.Uint64(hd[24:])),
	}}
	h := bf.h
	if h.grain > Day || h.start >= h.end || h.start%h.grain.Ms() != 0 || h.end%h.grain.Ms() != 0 ||
		h.covered <= h.start || h.covered > h.end || (h.end-h.start)/h.grain.Ms() > 1<<32 {
		return nil, errBadBlock
	}
	dataEnd := st.Size() - trailerLen
	ixOff := binary.LittleEndian.Uint64(tr[0:])
	ix := secRef{off: int64(ixOff), n: int64(binary.LittleEndian.Uint32(tr[8:])), crc: binary.LittleEndian.Uint32(tr[12:])}
	if ixOff < headerLen || ixOff > uint64(dataEnd) || ix.n < 1 || ix.n > dataEnd-ix.off {
		return nil, errBadBlock
	}
	raw, err := readSection(c, fh, ix)
	if err != nil {
		return nil, err
	}
	r := &reader{b: raw}
	// Every data section lies between the header and the index, and no two
	// overlap; counts are checked against what the file can hold before
	// they size anything.
	var refs []secRef
	getRef := func() secRef {
		off, n, crc := r.uvarint(), r.uvarint(), r.u32()
		if off < headerLen || n < 1 || off > ixOff || n > ixOff-off {
			r.err = errBadBlock
		}
		ref := secRef{off: int64(off), n: int64(n), crc: crc}
		refs = append(refs, ref)
		return ref
	}
	bf.dict = getRef()
	ndict := r.uvarint()
	ng := r.uvarint()
	if ndict > uint64(st.Size()) || ng > uint64(st.Size()) {
		return nil, errBadBlock
	}
	bf.ndict = int(ndict)
	for i := uint64(0); i < ng && r.err == nil; i++ {
		g := groupRef{owner: r.str(), family: r.str(), rows: int64(r.uvarint()), folded: int64(r.uvarint())}
		nc := r.uvarint()
		if g.rows < 1 || g.folded < 0 || g.folded > g.rows || g.rows > maxSection || nc < 2 || nc > uint64(st.Size()) {
			r.err = errBadBlock
			break
		}
		if n := len(bf.group); n > 0 {
			p := bf.group[n-1]
			if p.owner > g.owner || (p.owner == g.owner && p.family >= g.family) {
				r.err = errBadBlock
				break
			}
		}
		for j := uint64(0); j < nc && r.err == nil; j++ {
			g.cols = append(g.cols, getRef())
		}
		bf.group = append(bf.group, g)
	}
	if r.err != nil || len(r.b) != 0 {
		return nil, errBadBlock
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].off < refs[j].off })
	for i := 1; i < len(refs); i++ {
		if refs[i-1].off+refs[i-1].n > refs[i].off {
			return nil, errBadBlock
		}
	}
	return bf, nil
}

func equalKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lessKeys orders key lists element by element, without joining them.
func lessKeys(a, b []string) bool {
	for i := range a {
		if i >= len(b) {
			return false
		}
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// secBufs recycles the compressed and decoded bytes of sections: a query
// reads many columns, and each is turned into a decoded slice straight away.
var secBufs = sync.Pool{New: func() any { return new([]byte) }}

func getBuf() *[]byte { return secBufs.Get().(*[]byte) }
func putBuf(b *[]byte) {
	if cap(*b) <= 16<<20 { // let rare huge sections go to the GC
		secBufs.Put(b)
	}
}

// withSection decodes a section into a pooled buffer and passes it to fn; the
// bytes are only valid during fn.
func withSection(c *codec, fh io.ReaderAt, ref secRef, fn func(raw []byte) error) error {
	if ref.n < 1 || ref.n > maxSection {
		return errBadBlock
	}
	cb, db := getBuf(), getBuf()
	defer putBuf(cb)
	defer putBuf(db)
	if int64(cap(*cb)) < ref.n {
		*cb = make([]byte, ref.n)
	}
	comp := (*cb)[:ref.n]
	if _, err := fh.ReadAt(comp, ref.off); err != nil {
		return err
	}
	if crc32.ChecksumIEEE(comp) != ref.crc {
		return errBadBlock
	}
	// Exactly one frame, filling the section: nothing after it.
	if !oneFrame(comp) {
		return errBadBlock
	}
	out, err := c.dec.DecodeAll(comp, (*db)[:0])
	if err != nil {
		return errBadBlock
	}
	*db = out
	return fn(out)
}

// oneFrame reports whether b is exactly one Zstandard frame (RFC 8878
// section 3.1.1): its header, its blocks up to the last, and the checksum
// when the header asks for one.
func oneFrame(b []byte) bool {
	var h zstd.Header
	if h.Decode(b) != nil || h.Skippable || h.HeaderSize <= 0 {
		return false
	}
	i := h.HeaderSize
	for {
		if len(b)-i < 3 {
			return false
		}
		bh := uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16
		i += 3
		last, typ, size := bh&1 == 1, (bh>>1)&3, int(bh>>3)
		switch typ {
		case 0, 2: // raw, compressed: size bytes follow
		case 1: // RLE: one byte repeated size times
			size = 1
		default: // reserved
			return false
		}
		if size > len(b)-i {
			return false
		}
		i += size
		if last {
			break
		}
	}
	if h.HasCheckSum {
		i += 4
	}
	return i == len(b)
}

func readSection(c *codec, fh io.ReaderAt, ref secRef) ([]byte, error) {
	var out []byte
	err := withSection(c, fh, ref, func(raw []byte) error {
		out = append([]byte(nil), raw...)
		return nil
	})
	return out, err
}

// readRLE and readUvarints decode index columns; every value must be below
// limit (the dictionary size, or the block's buckets for the bucket column).
func readRLE(c *codec, fh io.ReaderAt, ref secRef, rows int64, limit uint64) (out []uint32, err error) {
	err = withSection(c, fh, ref, func(raw []byte) error {
		out, err = decodeRLE(raw, rows, limit)
		return err
	})
	return out, err
}

func readUvarints(c *codec, fh io.ReaderAt, ref secRef, rows int64, limit uint64) (out []uint32, err error) {
	err = withSection(c, fh, ref, func(raw []byte) error {
		out, err = decodeUvarints(raw, rows, limit)
		return err
	})
	return out, err
}

func readVarints(c *codec, fh io.ReaderAt, ref secRef, rows int64) (out []int64, err error) {
	err = withSection(c, fh, ref, func(raw []byte) error {
		out, err = decodeVarints(raw, rows)
		return err
	})
	return out, err
}

func readDict(c *codec, fh io.ReaderAt, bf *blockFile) (out []string, err error) {
	err = withSection(c, fh, bf.dict, func(raw []byte) error {
		out, err = parseDict(raw, bf)
		return err
	})
	return out, err
}

func parseDict(raw []byte, bf *blockFile) ([]string, error) {
	r := &reader{b: raw}
	n := int(r.uvarint())
	if n != bf.ndict || n > len(raw) {
		return nil, errBadBlock
	}
	out := make([]string, n)
	for i := range out {
		out[i] = r.str()
		// Sorted and distinct: comparing indexes compares strings.
		if i > 0 && r.err == nil && out[i-1] >= out[i] {
			return nil, errBadBlock
		}
	}
	if r.err != nil || len(r.b) != 0 {
		return nil, errBadBlock
	}
	return out, nil
}

// reader decodes varints and strings, remembering the first error.
type reader struct {
	b   []byte
	err error
}

func (r *reader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.err = errBadBlock
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *reader) varint() int64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Varint(r.b)
	if n <= 0 {
		r.err = errBadBlock
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *reader) u32() uint32 {
	if r.err != nil || len(r.b) < 4 {
		r.err = errBadBlock
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}

func (r *reader) str() string {
	n := r.uvarint()
	if r.err != nil || uint64(len(r.b)) < n {
		r.err = errBadBlock
		return ""
	}
	s := string(r.b[:n])
	r.b = r.b[n:]
	return s
}

// column decoders: each returns exactly rows values, with nothing after
// them, or errBadBlock.

// blockSpan is how many buckets a block's span holds: the bound on its
// bucket column.
func blockSpan(h blockHeader) uint64 { return uint64((h.end - h.start) / h.grain.Ms()) }

func decodeRLE(raw []byte, rows int64, limit uint64) ([]uint32, error) {
	out := make([]uint32, 0, rows)
	r := &reader{b: raw}
	for int64(len(out)) < rows && r.err == nil {
		v, run := r.uvarint(), r.uvarint()
		if r.err != nil || v >= limit || run < 1 || run > uint64(rows-int64(len(out))) {
			return nil, errBadBlock
		}
		for k := uint64(0); k < run; k++ {
			out = append(out, uint32(v))
		}
	}
	if r.err != nil || int64(len(out)) != rows || len(r.b) != 0 {
		return nil, errBadBlock
	}
	return out, nil
}

func decodeUvarints(raw []byte, rows int64, limit uint64) ([]uint32, error) {
	if int64(len(raw)) < rows {
		return nil, errBadBlock // every value takes at least a byte
	}
	out := make([]uint32, rows)
	r := &reader{b: raw}
	for i := range out {
		v := r.uvarint()
		if v >= limit {
			return nil, errBadBlock
		}
		out[i] = uint32(v)
	}
	if r.err != nil || len(r.b) != 0 {
		return nil, errBadBlock
	}
	return out, nil
}

func decodeVarints(raw []byte, rows int64) ([]int64, error) {
	if int64(len(raw)) < rows {
		return nil, errBadBlock
	}
	out := make([]int64, rows)
	r := &reader{b: raw}
	for i := range out {
		out[i] = r.varint()
	}
	if r.err != nil || len(r.b) != 0 {
		return nil, errBadBlock
	}
	return out, nil
}
