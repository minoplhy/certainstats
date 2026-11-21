package rblk

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// validDay is a UTC day start for blocks written directly.
const validDay = int64(1790812800000)

// goodBlock writes a valid block of generated records with si and returns its path.
func goodBlock(t *testing.T, si *schemaInfo) string {
	t.Helper()
	g := &gen{r: rand.New(rand.NewSource(5))}
	var recs []Record
	for i := range 200 {
		r := g.rec(validDay + int64(i)*60_000)
		r.ID = int64(i + 1)
		recs = append(recs, r)
	}
	path := filepath.Join(t.TempDir(), fmt.Sprintf("%d-1.rblk", validDay))
	if _, err := writeBlock(si, path, validDay, validDay+dayMs, 0, 1, map[string][]Record{"alice": recs[:120], "bob": recs[120:]}); err != nil {
		t.Fatal(err)
	}
	return path
}

// blockParts splits a block into its header and decoded sections.
func blockParts(t *testing.T, path string) (blockHeader, map[string][]byte) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hoff := binary.LittleEndian.Uint64(b[len(b)-20:])
	hlen := binary.LittleEndian.Uint32(b[len(b)-12:])
	var h blockHeader
	if err := json.Unmarshal(b[hoff:hoff+uint64(hlen)], &h); err != nil {
		t.Fatal(err)
	}
	secs := map[string][]byte{}
	for name, ref := range h.Secs {
		raw, err := codecs().dec.DecodeAll(b[ref.Off:ref.Off+ref.N], nil)
		if err != nil {
			t.Fatal(err)
		}
		secs[name] = raw
	}
	return h, secs
}

// assemble writes a block from a header and raw sections, filling in the
// section references; fix may change the header after that, and comp the
// compressed bytes of a section. hdr, when set, replaces the header bytes.
func assemble(t *testing.T, path string, h blockHeader, secs map[string][]byte, fix func(*blockHeader), comp func(name string, b []byte) []byte, hdr func([]byte) []byte) {
	t.Helper()
	names := make([]string, 0, len(secs))
	for n := range secs {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []byte
	h.Owners = append([]ownerRange(nil), h.Owners...) // fix may edit them
	h.Secs = map[string]secRef{}
	for _, n := range names {
		c := codecs().enc.EncodeAll(secs[n], nil)
		if comp != nil {
			c = comp(n, c)
		}
		h.Secs[n] = secRef{Off: int64(len(out)), N: int64(len(c)), CRC: crc32.ChecksumIEEE(c)}
		out = append(out, c...)
	}
	if fix != nil {
		fix(&h)
	}
	hb, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if hdr != nil {
		hb = hdr(hb)
	}
	hoff := len(out)
	out = append(out, hb...)
	var tr [trailerLen]byte
	binary.LittleEndian.PutUint64(tr[0:], uint64(hoff))
	binary.LittleEndian.PutUint32(tr[8:], uint32(len(hb)))
	binary.LittleEndian.PutUint32(tr[12:], crc32.ChecksumIEEE(hb))
	copy(tr[16:], blockMagic)
	if err := os.WriteFile(path, append(out, tr[:]...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readEvery decodes every row, column and child of a block.
func readEvery(si *schemaInfo, path string) error {
	bf, err := openBlockFile(si, path)
	if err != nil {
		return err
	}
	r, err := openReader(si, bf, nil)
	if err != nil {
		return err
	}
	defer r.close()
	raw, err := r.raw("dict")
	if err != nil {
		return err
	}
	if r.dict, err = readDict(raw); err != nil {
		return err
	}
	for i := range bf.h.Rows {
		if _, err := r.record(i, nil, true, nil); err != nil {
			return err
		}
	}
	return nil
}

func uvs(raw []byte) []uint64 {
	var out []uint64
	for len(raw) > 0 {
		u, k := binary.Uvarint(raw)
		out = append(out, u)
		raw = raw[k:]
	}
	return out
}

func putUvs(vs []uint64) []byte {
	var b []byte
	for _, v := range vs {
		b = binary.AppendUvarint(b, v)
	}
	return b
}

// TestBlockHeaderChecks: a header that passes its CRC but breaks Section 6
// or the section rules is a damaged block.
func TestBlockHeaderChecks(t *testing.T) {
	si, err := compile(testSchema())
	if err != nil {
		t.Fatal(err)
	}
	path := goodBlock(t, si)
	if err := readEvery(si, path); err != nil {
		t.Fatalf("good block: %v", err)
	}
	h, secs := blockParts(t, path)
	for name, c := range map[string]struct {
		fix func(*blockHeader)
		hdr func([]byte) []byte
	}{
		"owner ranges leave a gap": {fix: func(h *blockHeader) { h.Owners[1].Lo++ }},
		"owner ranges short":       {fix: func(h *blockHeader) { h.Rows++ }},
		"owners out of order":      {fix: func(h *blockHeader) { h.Owners[0].Owner, h.Owners[1].Owner = "zed", "amy" }},
		"min id above max id":      {fix: func(h *blockHeader) { h.Owners[0].MinID = h.Owners[0].MaxID + 1 }},
		"upto at the day's end":    {fix: func(h *blockHeader) { h.Upto = h.End }},
		"not a day":                {fix: func(h *blockHeader) { h.End-- }},
		"name does not match seq":  {fix: func(h *blockHeader) { h.Seq = 9 }},
		"missing section":          {fix: func(h *blockHeader) { delete(h.Secs, "c.n") }},
		"sections overlap":         {fix: func(h *blockHeader) { r := h.Secs["t"]; r.Off++; h.Secs["t"] = r }},
		"section past the data":    {fix: func(h *blockHeader) { r := h.Secs["t"]; r.N = 1 << 40; h.Secs["t"] = r }},
		"negative section offset":  {fix: func(h *blockHeader) { r := h.Secs["t"]; r.Off = -1; h.Secs["t"] = r }},
		"children negative":        {fix: func(h *blockHeader) { h.Children = -1 }},
		"end past int64":           {fix: func(h *blockHeader) { h.Start = (math.MaxInt64 / dayMs) * dayMs; h.End = h.Start + dayMs }},
		"empty owner":              {fix: func(h *blockHeader) { h.Owners[0].Owner = "" }},
		"repeated header member":   {hdr: func(b []byte) []byte { return append([]byte(`{"rows":1,`), b[1:]...) }},
		"repeated nested member": {hdr: func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"owner":"alice",`, `"owner":"alice","owner":"alice",`, 1))
		}},
		"fraction in an integer": {hdr: func(b []byte) []byte { return []byte(strings.Replace(string(b), `"seq":1,`, `"seq":1.5,`, 1)) }},
		"integer past int64 range": {hdr: func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"seq":1,`, `"seq":9223372036854775808,`, 1))
		}},
		"invalid UTF-8 in an owner": {hdr: func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"owner":"alice"`, "\"owner\":\"al\xffce\"", 1))
		}},
		"lone high surrogate": {hdr: func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"owner":"alice"`, `"owner":"al\uD800ce"`, 1))
		}},
		"lone low surrogate": {hdr: func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"owner":"alice"`, `"owner":"al\udc00ce"`, 1))
		}},
		"reversed surrogate pair": {hdr: func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"owner":"alice"`, `"owner":"\uDE00\uD83D"`, 1))
		}},
		"repeat spelled with an escape": {hdr: func(b []byte) []byte { return append([]byte(`{"\u0072ows":1,`), b[1:]...) }},
		"case variant of a name":        {hdr: func(b []byte) []byte { return append([]byte(`{"Rows":1,`), b[1:]...) }},
		"case variant, nested":          {hdr: func(b []byte) []byte { return []byte(strings.Replace(string(b), `"lo":`, `"LO":5,"lo":`, 1)) }},
		"case variant in a section ref": {hdr: func(b []byte) []byte { return []byte(strings.Replace(string(b), `{"o":`, `{"O":0,"o":`, 1)) }},
		"case variant in an owner": {hdr: func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"owner":"alice"`, `"OWNER":"x","owner":"alice"`, 1))
		}},
		"Unicode case variant": {hdr: func(b []byte) []byte { return append([]byte(`{"\u017feq":1,`), b[1:]...) }},
		"version 2":            {hdr: func(b []byte) []byte { return []byte(strings.Replace(string(b), `"v":1,`, `"v":2,`, 1)) }},
	} {
		assemble(t, path, h, secs, c.fix, nil, c.hdr)
		if _, err := openBlockFile(si, path); !errors.Is(err, errBadBlock) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A header that only reorders members is fine, and so is an unknown member.
	assemble(t, path, h, secs, nil, nil, func(b []byte) []byte { return append([]byte(`{"later":{"x":[1,{"x":2}]},`), b[1:]...) })
	if err := readEvery(si, path); err != nil {
		t.Fatalf("unknown member: %v", err)
	}
	// Inside an unknown member, names that look like defined ones are just
	// data: "Rows" there is not an alias of rows.
	assemble(t, path, h, secs, nil, nil, func(b []byte) []byte {
		return append([]byte(`{"extension":{"Rows":123,"owners":[{"LO":1}],"secs":{"x":{"O":1}}},`), b[1:]...)
	})
	if err := readEvery(si, path); err != nil {
		t.Fatalf("names inside an unknown member: %v", err)
	}
	// Escapes a writer may use are fine: a surrogate pair, an escaped
	// backslash before a "u", and Go's escapes of < > &.
	assemble(t, path, h, secs, nil, nil, func(b []byte) []byte {
		return append([]byte(`{"later":["\uD83D\uDE00","\\uD800","\u003c\u0026"],`), b[1:]...)
	})
	if err := readEvery(si, path); err != nil {
		t.Fatalf("valid escapes: %v", err)
	}
	// hlen past the file.
	b, _ := os.ReadFile(path)
	binary.LittleEndian.PutUint32(b[len(b)-12:], 1<<31)
	os.WriteFile(path, b, 0o644)
	if _, err := openBlockFile(si, path); !errors.Is(err, errBadBlock) {
		t.Errorf("huge hlen: %v", err)
	}
}

// TestBlockSectionChecks: sections whose values break Sections 7 and 8 fail
// when read, instead of reading as wrong values or panicking.
func TestBlockSectionChecks(t *testing.T) {
	si, err := compile(testSchema())
	if err != nil {
		t.Fatal(err)
	}
	path := goodBlock(t, si)
	h, secs := blockParts(t, path)
	ndict := uvs(secs["dict"])[0]
	edit := func(name string, f func([]uint64) []uint64) map[string][]byte {
		out := map[string][]byte{}
		for k, v := range secs {
			out[k] = v
		}
		out[name] = putUvs(f(uvs(secs[name])))
		return out
	}
	with := func(name string, raw []byte) map[string][]byte {
		out := map[string][]byte{}
		for k, v := range secs {
			out[k] = v
		}
		out[name] = raw
		return out
	}
	idSec := func(first []byte) []byte {
		raw := secs["c.uid"]
		_, k := binary.Uvarint(raw)
		l, _ := binary.Uvarint(raw)
		rest := raw[k+int(l):]
		return append(append(binary.AppendUvarint(nil, uint64(len(first))), first...), rest...)
	}
	cases := map[string]map[string][]byte{
		"code 0 without SameAs":     edit("c.cat", func(v []uint64) []uint64 { v[0] = 0; return v }),
		"code past the dict":        edit("c.cat", func(v []uint64) []uint64 { v[0] = ndict + 1; return v }),
		"child code 0, no SameAs":   edit("k.typ", func(v []uint64) []uint64 { v[0] = 0; return v }),
		"child count past children": edit("nchild", func(v []uint64) []uint64 { v[0] = 1 << 40; return v }),
		"uvarint past 64 bits":      with("id", append(append([]byte{}, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02), secs["id"]...)),
		"uvarint never ends":        with("id", []byte{0x80, 0x80}),
		"child counts off":          edit("nchild", func(v []uint64) []uint64 { v[0]++; return v }),
		"too few values":            edit("c.n", func(v []uint64) []uint64 { return v[1:] }),
		"byte after the values":     with("id", append(append([]byte{}, secs["id"]...), 0)),
		"empty id":                  with("c.uid", idSec([]byte{})),
		"unknown id form":           with("c.uid", idSec([]byte{5, 'x'})),
		"short 0x02 id":             with("c.uid", idSec([]byte{2, 0, 'x'})),
		"dict out of order":         with("dict", putDict("b", "a")),
		"dict repeats":              with("dict", putDict("a", "a")),
		"dict count too large":      with("dict", binary.AppendUvarint(nil, 1<<40)),
	}
	for name, s := range cases {
		assemble(t, path, h, s, nil, nil, nil)
		if err := readEvery(si, path); !errors.Is(err, errBadBlock) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Two zstd frames in one section.
	assemble(t, path, h, secs, nil, func(name string, c []byte) []byte {
		if name == "c.n" {
			return append(append([]byte{}, c...), c...)
		}
		return c
	}, nil)
	if err := readEvery(si, path); !errors.Is(err, errBadBlock) {
		t.Errorf("two frames: %v", err)
	}
	// A frame that needs a compression dictionary.
	denc, err := zstd.NewWriter(nil, zstd.WithEncoderDictRaw(7, []byte("some dictionary content, long enough to matter")))
	if err != nil {
		t.Fatal(err)
	}
	assemble(t, path, h, secs, nil, func(name string, c []byte) []byte {
		if name == "c.n" {
			return denc.EncodeAll(secs[name], nil)
		}
		return c
	}, nil)
	if err := readEvery(si, path); !errors.Is(err, errBadBlock) {
		t.Errorf("dictionary frame: %v", err)
	}
	// Text 0 means "the template" only where there is one.
	plain := testSchema()
	plain.Templates = nil
	psi, err := compile(plain)
	if err != nil {
		t.Fatal(err)
	}
	assemble(t, path, h, secs, nil, nil, nil)
	if err := readEvery(psi, path); !errors.Is(err, errBadBlock) {
		t.Errorf("template code without a template: %v", err)
	}
}

func putDict(ss ...string) []byte {
	b := binary.AppendUvarint(nil, uint64(len(ss)))
	for _, s := range ss {
		b = binary.AppendUvarint(b, uint64(len(s)))
		b = append(b, s...)
	}
	return b
}

// TestSchemaRules: SameAs cycles and repeated child names are refused, and a
// store refuses a schema whose stored bytes would read differently.
func TestSchemaRules(t *testing.T) {
	for name, mod := range map[string]func(*Schema){
		"SameAs itself":        func(s *Schema) { s.Columns[1].SameAs = "cat" },
		"SameAs cycle":         func(s *Schema) { s.Columns[1].SameAs = "host"; s.Columns[2].SameAs = "cat" },
		"child declared twice": func(s *Schema) { s.Children[1].Name = "typ"; s.Children[1].SameAs = "" },
	} {
		s := testSchema()
		mod(&s)
		if _, err := compile(s); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, eng := range []Engine{Slim, Blocks} {
		dir := t.TempDir()
		s, err := Open(dir, testSchema(), eng)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
		indexed := testSchema()
		indexed.Columns[1].Index = true // changes no stored byte
		if s, err = Open(dir, indexed, eng); err != nil {
			t.Fatalf("%s: new index: %v", eng, err)
		}
		s.Close()
		for name, mod := range map[string]func(*Schema){
			"kind":     func(s *Schema) { s.Columns[4].Kind = Bool },
			"SameAs":   func(s *Schema) { s.Columns[3].SameAs = "" },
			"template": func(s *Schema) { s.Templates = nil },
			"added":    func(s *Schema) { s.Columns = append(s.Columns, Column{Name: "more", Kind: Int}) },
			"renamed":  func(s *Schema) { s.Children[0].Name = "kind" },
		} {
			s := testSchema()
			mod(&s)
			if _, err := Open(dir, s, eng); !errors.Is(err, ErrShapeChanged) {
				t.Errorf("%s: %s changed: %v", eng, name, err)
			}
		}
	}
}

// TestOneWriter: a store has one owner at a time, the lock goes with Close,
// and an empty owner is refused.
func TestOneWriter(t *testing.T) {
	for _, eng := range []Engine{Slim, Blocks} {
		dir := t.TempDir()
		s, err := Open(dir, testSchema(), eng)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Open(dir, testSchema(), eng); !errors.Is(err, ErrLocked) {
			t.Fatalf("%s: second open: %v", eng, err)
		}
		g := &gen{r: rand.New(rand.NewSource(1))}
		if _, err := s.Insert(context.Background(), "", []Record{g.rec(testEnd)}); err == nil {
			t.Fatalf("%s: empty owner accepted", eng)
		}
		s.Close()
		// A refused open (here, a changed shape) releases the lock too.
		changed := testSchema()
		changed.Templates = nil
		if _, err := Open(dir, changed, eng); !errors.Is(err, ErrShapeChanged) {
			t.Fatalf("%s: changed shape: %v", eng, err)
		}
		s, err = Open(dir, testSchema(), eng)
		if err != nil {
			t.Fatalf("%s: reopen: %v", eng, err)
		}
		s.Close()
	}
}

// TestPruneDeletesDays: retention and owner removal delete whole blocks
// that keep no rows (the deletions are synced, so they cannot come back),
// and a removed owner's head rows go too.
func TestPruneDeletesDays(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir, testSchema(), Blocks)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g := &gen{r: rand.New(rand.NewSource(9))}
	var recs []Record
	for ts := testEnd - 3*dayMs; ts < testEnd; ts += 30 * 60_000 {
		recs = append(recs, g.rec(ts))
	}
	for _, o := range []string{"alice", "bob"} {
		if _, err := s.Insert(ctx, o, recs); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Maintain(ctx, testEnd+3*3600_000); err != nil {
		t.Fatal(err)
	}
	blocks := func() int {
		m, _ := filepath.Glob(filepath.Join(dir, "blocks", "*.rblk"))
		return len(m)
	}
	n := blocks()
	if n < 3 {
		t.Fatalf("%d blocks", n)
	}
	if err := s.Prune(ctx, testEnd-2*dayMs, []string{"alice", "bob"}); err != nil {
		t.Fatal(err)
	}
	if blocks() >= n {
		t.Fatalf("retention deleted no block: %d of %d", blocks(), n)
	}
	if _, err := s.Insert(ctx, "bob", []Record{g.rec(testEnd + 3*3600_000)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(ctx, 0, []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	if _, total, err := s.Find(ctx, Query{Owner: "bob"}); err != nil || total != 0 {
		t.Fatalf("bob after removal: %d (%v)", total, err)
	}
	if _, total, err := s.Find(ctx, Query{Owner: "alice"}); err != nil || total == 0 {
		t.Fatalf("alice after bob's removal: %d (%v)", total, err)
	}
}
