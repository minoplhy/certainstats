package rblk

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
)

// A block file holds one day of every owner's records:
//
//	sections (each zstd, CRC-checked) | header (JSON) | trailer
//
// Rows are sorted by owner, then time, then id. Sections: "dict" (sorted
// strings of every Dict column), "id" and "t" (delta varints), "nchild",
// one "c.<name>" per column and one "k.<name>" per child column (child rows
// flattened in row order). A Dict value is its dictionary index plus one, or
// 0 when it equals its SameAs column; Int values are delta varints; Text is
// 0 when the template rebuilds it, else length plus one and the bytes; an ID
// is its length and bytes (16 for a UUID).
const (
	blockMagic   = "RBK1"
	trailerLen   = 20
	blockVersion = 1
	// maxSection bounds a section's compressed and decoded size, so a
	// damaged block cannot make a reader allocate without bound.
	maxSection = 1 << 31
	// maxWindow bounds a frame's window, the decoder's history buffer, which
	// it allocates whatever the output size. The writer's frames use 8 MiB
	// or less.
	maxWindow = 1 << 27
	// maxHeader bounds the JSON header.
	maxHeader = 64 << 20
)

var errBadBlock = errors.New("rblk: damaged block")

type secRef struct {
	Off int64  `json:"o"`
	N   int64  `json:"n"`
	CRC uint32 `json:"c"`
}

type ownerRange struct {
	Owner string `json:"owner"`
	Lo    int    `json:"lo"`
	Hi    int    `json:"hi"`
	MinID int64  `json:"min_id"`
	MaxID int64  `json:"max_id"`
}

type blockHeader struct {
	Version   int   `json:"v"`
	Start     int64 `json:"start"`
	End       int64 `json:"end"`
	Seq       int64 `json:"seq"`
	Rows      int   `json:"rows"`
	Children  int   `json:"children"`
	MaxActive int64 `json:"max_active"`
	// Upto is where sealing stopped: the block holds the day's rows before
	// it (the rest are still in the head). 0 means the whole day.
	Upto   int64             `json:"upto,omitempty"`
	Owners []ownerRange      `json:"owners"`
	Secs   map[string]secRef `json:"secs"`
}

type blockFile struct {
	path string
	h    blockHeader
}

// sealed is the time before which the block holds every row of its day.
func (b *blockFile) sealed() int64 {
	if b.h.Upto == 0 {
		return b.h.End
	}
	return b.h.Upto
}

func (b *blockFile) owner(o string) (ownerRange, bool) {
	i := sort.Search(len(b.h.Owners), func(i int) bool { return b.h.Owners[i].Owner >= o })
	if i < len(b.h.Owners) && b.h.Owners[i].Owner == o {
		return b.h.Owners[i], true
	}
	return ownerRange{}, false
}

type codec struct {
	enc *zstd.Encoder
	dec *zstd.Decoder
}

var codecs = sync.OnceValue(func() *codec {
	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression), zstd.WithEncoderConcurrency(1), zstd.WithLowerEncoderMem(true))
	dec, _ := zstd.NewReader(nil, zstd.WithDecoderConcurrency(4), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxMemory(maxSection), zstd.WithDecoderMaxWindow(maxWindow))
	return &codec{enc: enc, dec: dec}
})

func zz(v int64) uint64   { return uint64(v<<1) ^ uint64(v>>63) }
func unzz(u uint64) int64 { return int64(u>>1) ^ -int64(u&1) }

// writeBlock writes owners' rows for [start, end) to path (via .tmp, fsync, rename).
func writeBlock(si *schemaInfo, path string, start, end, upto, seq int64, byOwner map[string][]Record) (*blockFile, error) {
	owners := make([]string, 0, len(byOwner))
	for o, list := range byOwner {
		if len(list) > 0 {
			owners = append(owners, o)
		}
	}
	sort.Strings(owners)
	var rows []*Record
	if upto >= end {
		upto = 0
	}
	h := blockHeader{Version: blockVersion, Start: start, End: end, Upto: upto, Seq: seq, Secs: map[string]secRef{}}
	for _, o := range owners {
		list := byOwner[o]
		sort.Slice(list, func(i, j int) bool {
			if list[i].Time != list[j].Time {
				return list[i].Time < list[j].Time
			}
			return list[i].ID < list[j].ID
		})
		or := ownerRange{Owner: o, Lo: len(rows), MinID: list[0].ID, MaxID: list[0].ID}
		for i := range list {
			rows = append(rows, &list[i])
			or.MinID, or.MaxID = min(or.MinID, list[i].ID), max(or.MaxID, list[i].ID)
		}
		or.Hi = len(rows)
		h.Owners = append(h.Owners, or)
	}
	h.Rows = len(rows)
	// The dictionary: every Dict value not covered by its SameAs column.
	dictSet := map[string]struct{}{}
	for _, r := range rows {
		for j, c := range si.Columns {
			if c.Kind == Dict && !(si.same[j] >= 0 && r.S[j] == r.S[si.same[j]]) {
				dictSet[r.S[j]] = struct{}{}
			}
		}
		for _, ch := range r.Children {
			for j, c := range si.Children {
				if c.Kind == Dict && !(si.csame[j] >= 0 && ch.S[j] == r.S[si.csame[j]]) {
					dictSet[ch.S[j]] = struct{}{}
				}
			}
		}
	}
	dict := make([]string, 0, len(dictSet))
	for s := range dictSet {
		dict = append(dict, s)
	}
	sort.Strings(dict)
	code := make(map[string]uint64, len(dict))
	for i, s := range dict {
		code[s] = uint64(i) + 1
	}
	var out bytes.Buffer
	c := codecs()
	sec := func(name string, raw []byte) {
		comp := c.enc.EncodeAll(raw, nil)
		h.Secs[name] = secRef{Off: int64(out.Len()), N: int64(len(comp)), CRC: crc32.ChecksumIEEE(comp)}
		out.Write(comp)
	}
	var b []byte
	b = binary.AppendUvarint(b[:0], uint64(len(dict)))
	for _, s := range dict {
		b = binary.AppendUvarint(b, uint64(len(s)))
		b = append(b, s...)
	}
	sec("dict", b)
	var prevID, prevT int64
	ids, ts, nch := []byte{}, []byte{}, []byte{}
	for _, r := range rows {
		ids = binary.AppendUvarint(ids, zz(r.ID-prevID))
		ts = binary.AppendUvarint(ts, zz(r.Time-prevT))
		nch = binary.AppendUvarint(nch, uint64(len(r.Children)))
		prevID, prevT = r.ID, r.Time
		h.Children += len(r.Children)
	}
	sec("id", ids)
	sec("t", ts)
	sec("nchild", nch)
	for j, col := range si.Columns {
		b = b[:0]
		var prev int64
		for _, r := range rows {
			switch col.Kind {
			case Dict:
				if s := si.same[j]; s >= 0 && r.S[j] == r.S[s] {
					b = append(b, 0)
				} else {
					b = binary.AppendUvarint(b, code[r.S[j]])
				}
			case Int, Bool:
				b = binary.AppendUvarint(b, zz(r.N[j]-prev))
				prev = r.N[j]
			case Text:
				if tpl := si.Templates[col.Name]; tpl != nil && tpl(r) == r.S[j] {
					b = append(b, 0)
				} else {
					b = binary.AppendUvarint(b, uint64(len(r.S[j]))+1)
					b = append(b, r.S[j]...)
				}
			case ID:
				id := encodeID(r.S[j])
				b = binary.AppendUvarint(b, uint64(len(id)))
				b = append(b, id...)
			}
		}
		sec("c."+col.Name, b)
	}
	for j, col := range si.Children {
		b = b[:0]
		var prev int64
		for _, r := range rows {
			for _, ch := range r.Children {
				switch col.Kind {
				case Dict:
					if s := si.csame[j]; s >= 0 && ch.S[j] == r.S[s] {
						b = append(b, 0)
					} else {
						b = binary.AppendUvarint(b, code[ch.S[j]])
					}
				case Int, Bool:
					b = binary.AppendUvarint(b, zz(ch.N[j]-prev))
					prev = ch.N[j]
					if col.Active {
						h.MaxActive = max(h.MaxActive, ch.N[j])
					}
				case Text:
					b = binary.AppendUvarint(b, uint64(len(ch.S[j]))+1)
					b = append(b, ch.S[j]...)
				case ID:
					id := encodeID(ch.S[j])
					b = binary.AppendUvarint(b, uint64(len(id)))
					b = append(b, id...)
				}
			}
		}
		sec("k."+col.Name, b)
	}
	hdr, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	hoff := int64(out.Len())
	out.Write(hdr)
	var tr [trailerLen]byte
	binary.LittleEndian.PutUint64(tr[0:], uint64(hoff))
	binary.LittleEndian.PutUint32(tr[8:], uint32(len(hdr)))
	binary.LittleEndian.PutUint32(tr[12:], crc32.ChecksumIEEE(hdr))
	copy(tr[16:], blockMagic)
	out.Write(tr[:])
	tmp := path + ".tmp"
	fh, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	if _, err := fh.Write(out.Bytes()); err != nil {
		fh.Close()
		return nil, err
	}
	if err := fh.Sync(); err != nil {
		fh.Close()
		return nil, err
	}
	if err := fh.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	// The rename must be on disk before the caller deletes the previous
	// block and the head forgets the sealed rows.
	if err := syncDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	return &blockFile{path: path, h: h}, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func openBlockFile(si *schemaInfo, path string) (*blockFile, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < trailerLen {
		return nil, errBadBlock
	}
	var tr [trailerLen]byte
	if _, err := fh.ReadAt(tr[:], st.Size()-trailerLen); err != nil {
		return nil, err
	}
	if string(tr[16:]) != blockMagic {
		return nil, errBadBlock
	}
	hoff, hlen := binary.LittleEndian.Uint64(tr[0:]), int64(binary.LittleEndian.Uint32(tr[8:]))
	if hlen < 2 || hlen > maxHeader || hoff > uint64(st.Size()) || int64(hoff)+hlen+trailerLen != st.Size() {
		return nil, errBadBlock
	}
	hdr := make([]byte, hlen)
	if _, err := fh.ReadAt(hdr, int64(hoff)); err != nil {
		return nil, err
	}
	if crc32.ChecksumIEEE(hdr) != binary.LittleEndian.Uint32(tr[12:]) {
		return nil, errBadBlock
	}
	b := &blockFile{path: path}
	// Go's JSON decoder would replace invalid UTF-8 and unpaired surrogate
	// escapes with U+FFFD instead of failing, changing an owner's name.
	if !utf8.Valid(hdr) || loneSurrogate(hdr) || dupKeys(hdr) || json.Unmarshal(hdr, &b.h) != nil || b.h.Version != blockVersion {
		return nil, errBadBlock
	}
	if err := checkHeader(si, &b.h, int64(hoff)); err != nil {
		return nil, err
	}
	if filepath.Base(path) != fmt.Sprintf("%d-%d.rblk", b.h.Start, b.h.Seq) {
		return nil, fmt.Errorf("%w: name does not match start and seq", errBadBlock)
	}
	return b, nil
}

// checkHeader applies Section 6 and the section rules of Section 10.
func checkHeader(si *schemaInfo, h *blockHeader, hoff int64) error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", errBadBlock, why) }
	if h.Start < 0 || h.Start%dayMs != 0 || h.Start > math.MaxInt64-dayMs || h.End != h.Start+dayMs {
		return bad("not one UTC day")
	}
	if h.Seq < 1 || h.Rows < 1 || h.Children < 0 || h.Rows > maxSection || h.Children > maxSection {
		return bad("counts out of range")
	}
	if h.Upto != 0 && (h.Upto <= h.Start || h.Upto >= h.End) {
		return bad("upto outside the day")
	}
	next := 0
	for i, o := range h.Owners {
		if o.Owner == "" || o.Lo != next || o.Hi <= o.Lo || o.MinID > o.MaxID || (i > 0 && h.Owners[i-1].Owner >= o.Owner) {
			return bad("owner ranges")
		}
		next = o.Hi
	}
	if next != h.Rows {
		return bad("owner ranges do not cover the rows")
	}
	need := []string{"dict", "id", "t", "nchild"}
	for _, c := range si.Columns {
		need = append(need, "c."+c.Name)
	}
	for _, c := range si.Children {
		need = append(need, "k."+c.Name)
	}
	for _, name := range need {
		if _, ok := h.Secs[name]; !ok {
			return bad("no section " + name)
		}
	}
	refs := make([]secRef, 0, len(h.Secs))
	for _, ref := range h.Secs {
		if ref.Off < 0 || ref.N < 1 || ref.N > maxSection || ref.Off > hoff-ref.N {
			return bad("section outside the data")
		}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Off < refs[j].Off })
	for i := 1; i < len(refs); i++ {
		if refs[i].Off < refs[i-1].Off+refs[i-1].N {
			return bad("sections overlap")
		}
	}
	return nil
}

// loneSurrogate reports whether a JSON string in b holds a \u escape of a
// UTF-16 surrogate that is not a high surrogate followed by a \u escape of
// a low one.
func loneSurrogate(b []byte) bool {
	hex4 := func(i int) (rune, bool) {
		if i+6 > len(b) || b[i] != '\\' || b[i+1] != 'u' {
			return 0, false
		}
		var r rune
		for _, c := range b[i+2 : i+6] {
			switch {
			case c >= '0' && c <= '9':
				r = r<<4 | rune(c-'0')
			case c >= 'a' && c <= 'f':
				r = r<<4 | rune(c-'a'+10)
			case c >= 'A' && c <= 'F':
				r = r<<4 | rune(c-'A'+10)
			default:
				return 0, false
			}
		}
		return r, true
	}
	in := false
	for i := 0; i < len(b); i++ {
		switch {
		case !in:
			in = b[i] == '"'
		case b[i] == '"':
			in = false
		case b[i] == '\\':
			r, ok := hex4(i)
			if !ok {
				i++ // a one-character escape: skip the escaped byte
				continue
			}
			switch {
			case r >= 0xDC00 && r <= 0xDFFF:
				return true
			case r >= 0xD800 && r <= 0xDBFF:
				lo, ok := hex4(i + 6)
				if !ok || lo < 0xDC00 || lo > 0xDFFF {
					return true
				}
				i += 6
			}
			i += 5
		}
	}
	return false
}

// The member names Section 6 defines, per object. Go's decoder matches
// struct fields without regard to case (Unicode folding, as
// strings.EqualFold), so in these objects a name that differs from a
// defined one only by case would be read as it; such names are refused.
var (
	headerNames = []string{"v", "start", "end", "seq", "rows", "children", "max_active", "upto", "owners", "secs"}
	ownerNames  = []string{"owner", "lo", "hi", "min_id", "max_id"}
	secRefNames = []string{"o", "n", "c"}
)

func caseVariant(k string, names []string) bool {
	for _, n := range names {
		if k != n && strings.EqualFold(k, n) {
			return true
		}
	}
	return false
}

// dupKeys reports whether any JSON object in b repeats a member name (as
// decoded, so escapes count as their characters), or whether the header,
// an owner entry or a section reference holds a case variant of one of its
// defined names. Values inside unknown members are only checked for repeats.
func dupKeys(b []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(b))
	// kind says which defined object (or array of them) a container is.
	const (
		other = iota
		header
		owners // the owners array
		owner
		secs // the secs object: its names are section names
		secRef
	)
	type frame struct {
		obj   bool
		kind  int
		key   bool   // the next string token is a member name
		last  string // the member whose value comes next
		seen  map[string]bool
		names []string
	}
	var stack []*frame
	// child is the kind of a container opened as the next value in top.
	child := func(top *frame, obj bool) int {
		switch {
		case top == nil:
			if obj {
				return header
			}
		case top.kind == header && top.last == "owners" && !obj:
			return owners
		case top.kind == header && top.last == "secs" && obj:
			return secs
		case top.kind == owners && obj:
			return owner
		case top.kind == secs && obj:
			return secRef
		}
		return other
	}
	valueEnded := func() {
		if len(stack) > 0 && stack[len(stack)-1].obj {
			stack[len(stack)-1].key = true
		}
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return true
		}
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if top != nil && top.obj && top.key {
			k, ok := tok.(string)
			if !ok { // the closing brace
				stack = stack[:len(stack)-1]
				valueEnded()
				continue
			}
			if top.seen[k] || caseVariant(k, top.names) {
				return true
			}
			top.seen[k] = true
			top.key, top.last = false, k
			continue
		}
		switch tok {
		case json.Delim('{'):
			f := &frame{obj: true, kind: child(top, true), key: true, seen: map[string]bool{}}
			switch f.kind {
			case header:
				f.names = headerNames
			case owner:
				f.names = ownerNames
			case secRef:
				f.names = secRefNames
			}
			stack = append(stack, f)
			continue
		case json.Delim('['):
			stack = append(stack, &frame{kind: child(top, false)})
			continue
		case json.Delim(']'):
			stack = stack[:len(stack)-1]
		}
		valueEnded()
	}
}

// oneFrame reports whether b is exactly one Zstandard frame (RFC 8878
// section 3.1.1): its header, its blocks up to the last, and the checksum
// when the header asks for one.
func oneFrame(b []byte) bool {
	var h zstd.Header
	// A frame that names a compression dictionary cannot be decoded: rblk
	// has no way to ship one (Section 4.6).
	if h.Decode(b) != nil || h.Skippable || h.HeaderSize <= 0 || h.DictionaryID != 0 {
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

// reader decodes a block's sections on demand, each at most once.
type reader struct {
	si   *schemaInfo
	bf   *blockFile
	fh   *os.File
	dict []string
	cols map[string]any
	// cache, when set, keeps decoded sections across readers.
	cache *secCache
	// codes[j] is Dict column j's codes with SameAs already followed.
	codes [][]uint32
	// child offsets: kids[i] is row i's first child
	kids []int
}

func openReader(si *schemaInfo, bf *blockFile, dict []string) (*reader, error) {
	fh, err := os.Open(bf.path)
	if err != nil {
		return nil, err
	}
	return &reader{si: si, bf: bf, fh: fh, dict: dict, cols: map[string]any{}}, nil
}

func (r *reader) close() { r.fh.Close() }

func (r *reader) cached(name string) (any, bool) {
	if v, ok := r.cols[name]; ok {
		return v, true
	}
	if v, ok := r.cache.get(r.bf.path, name); ok {
		r.cols[name] = v
		return v, true
	}
	return nil, false
}

func (r *reader) keep(name string, v any, size int64) {
	r.cols[name] = v
	r.cache.put(r.bf.path, name, v, size)
}

// secCache keeps decoded sections of sealed blocks across queries. Blocks
// never change once written, so an entry is right until its file goes away;
// the engine drops a file's entries when it replaces or deletes the file.
// Oldest entries go first once the decoded size passes max.
type secCache struct {
	mu    sync.Mutex
	max   int64
	size  int64
	items map[[2]string]*secItem
	order [][2]string // insertion order, oldest first; may hold dropped keys
}

type secItem struct {
	v    any
	size int64
}

func newSecCache(max int64) *secCache {
	return &secCache{max: max, items: map[[2]string]*secItem{}}
}

func (c *secCache) get(path, name string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[[2]string{path, name}]
	if !ok {
		return nil, false
	}
	return it.v, true
}

func (c *secCache) put(path, name string, v any, size int64) {
	if c == nil || size > c.max {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := [2]string{path, name}
	if _, ok := c.items[k]; ok {
		return
	}
	c.items[k] = &secItem{v, size}
	c.order = append(c.order, k)
	c.size += size
	for c.size > c.max && len(c.order) > 0 {
		old := c.order[0]
		c.order = c.order[1:]
		if it, ok := c.items[old]; ok {
			c.size -= it.size
			delete(c.items, old)
		}
	}
}

// drop forgets every section of one block file.
func (c *secCache) drop(path string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, it := range c.items {
		if k[0] == path {
			c.size -= it.size
			delete(c.items, k)
		}
	}
	keep := c.order[:0]
	for _, k := range c.order {
		if _, ok := c.items[k]; ok {
			keep = append(keep, k)
		}
	}
	c.order = keep
}

func (r *reader) raw(name string) ([]byte, error) {
	ref, ok := r.bf.h.Secs[name]
	if !ok {
		return nil, fmt.Errorf("%w: no section %s", errBadBlock, name)
	}
	comp := make([]byte, ref.N)
	if _, err := r.fh.ReadAt(comp, ref.Off); err != nil && err != io.EOF {
		return nil, err
	}
	if crc32.ChecksumIEEE(comp) != ref.CRC || !oneFrame(comp) {
		return nil, errBadBlock
	}
	out, err := codecs().dec.DecodeAll(comp, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errBadBlock, err)
	}
	return out, nil
}

func readDict(raw []byte) ([]string, error) {
	n, k := binary.Uvarint(raw)
	if k <= 0 {
		return nil, errBadBlock
	}
	raw = raw[k:]
	if n > uint64(len(raw)) { // every string takes at least its length byte
		return nil, errBadBlock
	}
	out := make([]string, 0, n)
	for range n {
		l, k := binary.Uvarint(raw)
		if k <= 0 || uint64(len(raw)-k) < l {
			return nil, errBadBlock
		}
		s := string(raw[k : k+int(l)])
		if len(out) > 0 && out[len(out)-1] >= s {
			return nil, fmt.Errorf("%w: dictionary not sorted or repeats", errBadBlock)
		}
		out = append(out, s)
		raw = raw[k+int(l):]
	}
	if len(raw) != 0 {
		return nil, errBadBlock
	}
	return out, nil
}

// ints decodes a delta-varint section of n values.
func (r *reader) ints(name string, n int) ([]int64, error) {
	if v, ok := r.cached(name); ok {
		return v.([]int64), nil
	}
	raw, err := r.raw(name)
	if err != nil {
		return nil, err
	}
	if n > len(raw) {
		return nil, errBadBlock
	}
	out := make([]int64, n)
	var prev int64
	for i := range out {
		u, k := binary.Uvarint(raw)
		if k <= 0 {
			return nil, errBadBlock
		}
		prev += unzz(u) // two's complement: wraps at 64 bits, as written
		out[i] = prev
		raw = raw[k:]
	}
	if len(raw) != 0 {
		return nil, errBadBlock
	}
	r.keep(name, out, int64(n)*8)
	return out, nil
}

// uvarints decodes a plain uvarint section of n values (Dict codes, child
// counts), each at most limit, and 0 only when zero is true.
func (r *reader) uvarints(name string, n int, limit uint64, zero bool) ([]uint32, error) {
	if v, ok := r.cached(name); ok {
		return v.([]uint32), nil
	}
	raw, err := r.raw(name)
	if err != nil {
		return nil, err
	}
	if n > len(raw) {
		return nil, errBadBlock
	}
	out := make([]uint32, n)
	for i := range out {
		u, k := binary.Uvarint(raw)
		if k <= 0 || u > limit || (u == 0 && !zero) {
			return nil, fmt.Errorf("%w: %s value out of range", errBadBlock, name)
		}
		out[i] = uint32(u)
		raw = raw[k:]
	}
	if len(raw) != 0 {
		return nil, errBadBlock
	}
	r.keep(name, out, int64(n)*4)
	return out, nil
}

// codesOf decodes a Dict section: codes up to the dictionary size, and 0
// only for a column with SameAs.
func (r *reader) codesOf(name string, n int, same bool) ([]uint32, error) {
	return r.uvarints(name, n, uint64(len(r.dict)), same)
}

// nchild decodes the child counts, which must sum to the header's children.
func (r *reader) nchild() ([]uint32, error) {
	n, err := r.uvarints("nchild", r.bf.h.Rows, uint64(r.bf.h.Children), true)
	if err != nil {
		return nil, err
	}
	// Each count is checked against what is left before it is added, so
	// the running sum never passes children and cannot overflow.
	left := r.bf.h.Children
	for _, c := range n {
		if int(c) > left {
			return nil, fmt.Errorf("%w: child counts exceed children", errBadBlock)
		}
		left -= int(c)
	}
	if left != 0 {
		return nil, fmt.Errorf("%w: child counts do not sum to children", errBadBlock)
	}
	return n, nil
}

// Blob section kinds.
const (
	blobID       = iota // ID values (Section 8.4)
	blobText            // Text without a template: always length plus one
	blobTemplate        // Text with a template: 0 means "the template"
)

// blobs decodes a length-prefixed section of n values of one kind.
func (r *reader) blobs(name string, n int, kind int) ([][]byte, error) {
	if v, ok := r.cached(name); ok {
		return v.([][]byte), nil
	}
	raw, err := r.raw(name)
	if err != nil {
		return nil, err
	}
	full := int64(len(raw))
	if n > len(raw) {
		return nil, errBadBlock
	}
	out := make([][]byte, n)
	for i := range out {
		l, k := binary.Uvarint(raw)
		if k <= 0 {
			return nil, errBadBlock
		}
		raw = raw[k:]
		if kind != blobID {
			if l == 0 {
				if kind == blobText {
					return nil, fmt.Errorf("%w: %s has no template", errBadBlock, name)
				}
				continue // nil: rebuilt by the template
			}
			l--
		}
		if uint64(len(raw)) < l {
			return nil, errBadBlock
		}
		out[i] = raw[:l:l]
		if out[i] == nil {
			out[i] = []byte{}
		}
		if kind == blobID && !validID(out[i]) {
			return nil, fmt.Errorf("%w: %s holds a malformed id", errBadBlock, name)
		}
		raw = raw[l:]
	}
	if len(raw) != 0 {
		return nil, errBadBlock
	}
	r.keep(name, out, full+int64(n)*24)
	return out, nil
}

func (r *reader) childStarts() ([]int, error) {
	if r.kids != nil {
		return r.kids, nil
	}
	n, err := r.nchild()
	if err != nil {
		return nil, err
	}
	r.kids = make([]int, len(n)+1)
	for i, c := range n {
		r.kids[i+1] = r.kids[i] + int(c)
	}
	return r.kids, nil
}

// dictCodes returns Dict column j's code for every row, SameAs followed.
// Hot loops take this slice once instead of calling dictCode per row.
func (r *reader) dictCodes(j int) ([]uint32, error) {
	if r.codes == nil {
		r.codes = make([][]uint32, len(r.si.Columns))
	}
	if c := r.codes[j]; c != nil {
		return c, nil
	}
	n := r.bf.h.Rows
	name := "r." + r.si.Columns[j].Name // resolved codes, cached like a section
	if v, ok := r.cached(name); ok {
		r.codes[j] = v.([]uint32)
		return r.codes[j], nil
	}
	codes, err := r.codesOf("c."+r.si.Columns[j].Name, n, r.si.same[j] >= 0)
	if err != nil {
		return nil, err
	}
	if s := r.si.same[j]; s >= 0 {
		parent, err := r.dictCodes(s)
		if err != nil {
			return nil, err
		}
		out := make([]uint32, n)
		for i, c := range codes {
			if c == 0 {
				c = parent[i]
			}
			out[i] = c
		}
		codes = out
		r.keep(name, codes, int64(n)*4)
	}
	r.codes[j] = codes
	return codes, nil
}

// dictCode resolves a Dict column's code at row i, following SameAs.
func (r *reader) dictCode(j, i int) (uint32, error) {
	codes, err := r.dictCodes(j)
	if err != nil {
		return 0, err
	}
	return codes[i], nil
}

func (r *reader) dictStr(code uint32) string {
	if code == 0 || int(code) > len(r.dict) {
		return ""
	}
	return r.dict[code-1]
}

// record materializes row i; cols limits which columns are filled (nil: all).
func (r *reader) record(i int, cols map[int]bool, children bool, keep func(*Child) bool) (Record, error) {
	si := r.si
	n := r.bf.h.Rows
	ids, err := r.ints("id", n)
	if err != nil {
		return Record{}, err
	}
	ts, err := r.ints("t", n)
	if err != nil {
		return Record{}, err
	}
	rec := Record{ID: ids[i], Time: ts[i], S: make([]string, len(si.Columns)), N: make([]int64, len(si.Columns))}
	// A templated Text column needs the columns its template reads: fill all.
	for j := range cols {
		if si.Columns[j].Kind == Text {
			cols = nil
			break
		}
	}
	want := func(j int) bool { return cols == nil || cols[j] }
	for j, c := range si.Columns {
		if !want(j) {
			continue
		}
		switch c.Kind {
		case Dict:
			code, err := r.dictCode(j, i)
			if err != nil {
				return rec, err
			}
			rec.S[j] = r.dictStr(code)
		case Int, Bool:
			v, err := r.ints("c."+c.Name, n)
			if err != nil {
				return rec, err
			}
			rec.N[j] = v[i]
		case ID:
			v, err := r.blobs("c."+c.Name, n, blobID)
			if err != nil {
				return rec, err
			}
			rec.S[j] = decodeID(v[i])
		}
	}
	for j, c := range si.Columns {
		if c.Kind != Text || !want(j) {
			continue
		}
		kind := blobText
		if si.Templates[c.Name] != nil {
			kind = blobTemplate
		}
		v, err := r.blobs("c."+c.Name, n, kind)
		if err != nil {
			return rec, err
		}
		if v[i] != nil {
			rec.S[j] = string(v[i])
		} else if tpl := si.Templates[c.Name]; tpl != nil {
			rec.S[j] = tpl(&rec)
		}
	}
	if children && len(si.Children) > 0 {
		starts, err := r.childStarts()
		if err != nil {
			return rec, err
		}
		total := r.bf.h.Children
		for k := starts[i]; k < starts[i+1]; k++ {
			ch := Child{S: make([]string, len(si.Children)), N: make([]int64, len(si.Children))}
			for j, c := range si.Children {
				switch c.Kind {
				case Dict:
					codes, err := r.codesOf("k."+c.Name, total, si.csame[j] >= 0)
					if err != nil {
						return rec, err
					}
					if codes[k] == 0 && si.csame[j] >= 0 {
						code, err := r.dictCode(si.csame[j], i)
						if err != nil {
							return rec, err
						}
						ch.S[j] = r.dictStr(code)
					} else {
						ch.S[j] = r.dictStr(codes[k])
					}
				case Int, Bool:
					v, err := r.ints("k."+c.Name, total)
					if err != nil {
						return rec, err
					}
					ch.N[j] = v[k]
				case Text:
					v, err := r.blobs("k."+c.Name, total, blobText)
					if err != nil {
						return rec, err
					}
					ch.S[j] = string(v[k])
				case ID:
					v, err := r.blobs("k."+c.Name, total, blobID)
					if err != nil {
						return rec, err
					}
					ch.S[j] = decodeID(v[k])
				}
			}
			if keep == nil || keep(&ch) {
				rec.Children = append(rec.Children, ch)
			}
		}
	}
	return rec, nil
}
