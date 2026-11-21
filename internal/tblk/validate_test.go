package tblk

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestColumnChecks: column decoders reject values past their bound, runs
// that overshoot or are empty, and bytes after the last value.
func TestColumnChecks(t *testing.T) {
	uv := func(vs ...uint64) []byte {
		var b []byte
		for _, v := range vs {
			b = binary.AppendUvarint(b, v)
		}
		return b
	}
	if _, err := decodeUvarints(uv(0, 4, 2), 3, 5); err != nil {
		t.Fatalf("good uvarints: %v", err)
	}
	for name, c := range map[string]struct {
		raw   []byte
		rows  int64
		limit uint64
	}{
		"index past the dictionary": {uv(0, 5), 2, 5},
		"bytes after the values":    {uv(0, 1, 1), 2, 5},
		"too few values":            {uv(0), 2, 5},
	} {
		if _, err := decodeUvarints(c.raw, c.rows, c.limit); err == nil {
			t.Errorf("uvarints, %s: accepted", name)
		}
	}
	if _, err := decodeRLE(uv(1, 2, 0, 1), 3, 2); err != nil {
		t.Fatalf("good runs: %v", err)
	}
	for name, raw := range map[string][]byte{
		"run past rows":         uv(1, 4),
		"empty run":             uv(1, 0, 0, 3),
		"index past dictionary": uv(2, 3),
		"bytes after the runs":  uv(1, 3, 0),
	} {
		if _, err := decodeRLE(raw, 3, 2); err == nil {
			t.Errorf("runs, %s: accepted", name)
		}
	}
	vb := binary.AppendVarint(binary.AppendVarint(nil, -3), 7)
	if _, err := decodeVarints(append(vb, 0), 2); err == nil {
		t.Error("varints with a trailing byte accepted")
	}
	dict := func(ss ...string) []byte {
		b := binary.AppendUvarint(nil, uint64(len(ss)))
		for _, s := range ss {
			b = binary.AppendUvarint(b, uint64(len(s)))
			b = append(b, s...)
		}
		return b
	}
	if _, err := parseDict(dict("a", "b"), &blockFile{ndict: 2}); err != nil {
		t.Fatalf("good dictionary: %v", err)
	}
	for name, raw := range map[string][]byte{
		"unsorted":    dict("b", "a"),
		"repeated":    dict("a", "a"),
		"trailing":    append(dict("a", "b"), 0),
		"wrong count": dict("a"),
	} {
		if _, err := parseDict(raw, &blockFile{ndict: 2}); err == nil {
			t.Errorf("dictionary, %s: accepted", name)
		}
	}
}

// TestOneFrame: a section is exactly one zstd frame, whatever its size.
func TestOneFrame(t *testing.T) {
	c, err := newCodec()
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	for _, n := range []int{1, 12, 300, 1 << 18} {
		raw := make([]byte, n)
		for i := range raw {
			raw[i] = byte(i * 7)
		}
		f := c.enc.EncodeAll(raw, nil)
		if !oneFrame(f) {
			t.Fatalf("%d bytes: one frame refused", n)
		}
		if oneFrame(append(append([]byte{}, f...), f...)) {
			t.Errorf("%d bytes: two frames accepted", n)
		}
		if oneFrame(append(append([]byte{}, f...), 0)) {
			t.Errorf("%d bytes: a trailing byte accepted", n)
		}
		if oneFrame(f[:len(f)-1]) {
			t.Errorf("%d bytes: a cut frame accepted", n)
		}
	}
}

// TestHeaderChecks: a block whose header passes its CRC but breaks the
// header rules (misaligned, covered past end) is damaged.
func TestHeaderChecks(t *testing.T) {
	si, err := compile(dnsSchema())
	if err != nil {
		t.Fatal(err)
	}
	c, err := newCodec()
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	dir := t.TempDir()
	h := blockHeader{grain: Minute, start: testEnd, end: testEnd + hourMs, covered: testEnd + hourMs}
	path := filepath.Join(dir, "good.blk")
	if _, err := writeBlock(c, si, path, h, recsFor(t, si, "alice", []string{"c1"}, testEnd, 5, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := openBlock(c, path); err != nil {
		t.Fatalf("good block: %v", err)
	}
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(b []byte){
		"start not on the grain":  func(b []byte) { binary.LittleEndian.PutUint64(b[8:], uint64(testEnd+1)) },
		"covered past end":        func(b []byte) { binary.LittleEndian.PutUint64(b[24:], uint64(testEnd+2*hourMs)) },
		"end before start":        func(b []byte) { binary.LittleEndian.PutUint64(b[16:], uint64(testEnd-hourMs)) },
		"grain out of range":      func(b []byte) { b[5] = 3 },
		"index offset in header":  func(b []byte) { binary.LittleEndian.PutUint64(b[len(b)-24:], 8) },
		"index runs into trailer": func(b []byte) { binary.LittleEndian.PutUint32(b[len(b)-16:], 1<<20) },
	} {
		b := append([]byte{}, good...)
		mod(b)
		binary.LittleEndian.PutUint32(b[len(b)-8:], crc32.ChecksumIEEE(b[:headerLen]))
		p := filepath.Join(dir, "bad.blk")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := openBlock(c, p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestInputRules: Other in a key that does not fold, and negative counts in
// a bounded family, are refused at the door; a bound row is not.
func TestInputRules(t *testing.T) {
	si, err := compile(boundSchema(2))
	if err != nil {
		t.Fatal(err)
	}
	f := si.byName["names"]
	got := toRecs(f, "c", 0, []Row{
		{Keys: []string{"z", "a"}, Values: []int64{3, 0}},
		{Keys: []string{Other, "b"}, Values: []int64{3, 0}},   // Other in the zone
		{Keys: []string{"z", Other}, Values: []int64{2, 0}},   // a folded row
		{Keys: []string{Other, Other}, Values: []int64{0, 4}}, // the bound row
		{Keys: []string{"z", "c"}, Values: []int64{-1, 0}},    // negative rank measure
		{Keys: []string{Other, Other}, Values: []int64{0, -2}},
	})
	if len(got) != 3 || got[0].keys[1] != "a" || got[1].keys[1] != Other || got[2].vals[1] != 4 {
		t.Fatalf("kept %+v", got)
	}
	if satAdd(math.MaxInt64-1, 5) != math.MaxInt64 || satAdd(math.MinInt64+1, -5) != math.MinInt64 || satAdd(2, -5) != -3 {
		t.Fatal("satAdd")
	}
}

// TestShapeAndLock: a store refuses a family whose shape changed, accepts
// new families and new caps, and has one owner at a time.
func TestShapeAndLock(t *testing.T) {
	for _, eng := range []Engine{SQL, Blocks} {
		dir := t.TempDir()
		s, err := Open(dir, boundSchema(2), Options{Engine: eng})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Open(dir, boundSchema(2), Options{Engine: eng}); !errors.Is(err, ErrLocked) {
			t.Fatalf("%s: second open: %v", eng, err)
		}
		if err := s.Append(context.Background(), "u", "c", "names", testEnd, []Row{{Keys: []string{"z", "a"}, Values: []int64{1, 0}}}); err != nil {
			t.Fatal(err)
		}
		// A refused row is counted, not silent.
		if err := s.Append(context.Background(), "u", "c", "names", testEnd, []Row{{Keys: []string{Other, "b"}, Values: []int64{1, 0}}}); err != nil {
			t.Fatal(err)
		}
		if st, err := s.Stats(context.Background()); err != nil || st.Dropped["names"] != 1 {
			t.Fatalf("%s: dropped %v (%v)", eng, st.Dropped, err)
		}
		s.Close()

		grown := boundSchema(5) // a new cap
		grown.Families = append(grown.Families, Family{Name: "more", Keys: []Key{{Name: "k"}}, Measures: []string{"n"}})
		s, err = Open(dir, grown, Options{Engine: eng})
		if err != nil {
			t.Fatalf("%s: new cap and family: %v", eng, err)
		}
		s.Close()

		swapped := boundSchema(2)
		swapped.Families[0].Measures = []string{"err", "count"}
		swapped.Families[0].Bound = "count"
		if _, err := Open(dir, swapped, Options{Engine: eng}); !errors.Is(err, ErrShapeChanged) {
			t.Fatalf("%s: swapped measures: %v", eng, err)
		}
		renamed := boundSchema(2)
		renamed.Families[0].Keys[0].Name = "zone2"
		if _, err := Open(dir, renamed, Options{Engine: eng}); !errors.Is(err, ErrShapeChanged) {
			t.Fatalf("%s: renamed key: %v", eng, err)
		}
		// A refused open releases the lock.
		s, err = Open(dir, boundSchema(2), Options{Engine: eng})
		if err != nil {
			t.Fatalf("%s: reopen: %v", eng, err)
		}
		s.Close()
	}
}

// TestKeysWithNUL: keys holding 0x00 stay distinct when rows are summed and
// folded, and order key by key. Joining keys with a 0x00 separator once made
// ("a\x00b", "c") and ("a", "b\x00c") one row.
func TestKeysWithNUL(t *testing.T) {
	x := rec{collector: "c", keys: []string{"a\x00b", "c"}, vals: []int64{1, 0}}
	y := rec{collector: "c", keys: []string{"a", "b\x00c"}, vals: []int64{2, 0}}
	if keyID(x.keys) == keyID(y.keys) {
		t.Fatal("keyID merges the two tuples")
	}
	if got := sumRecs([]rec{x, y, x}, 0); len(got) != 2 {
		t.Fatalf("sumRecs merged distinct tuples: %+v", got)
	}
	if !lessKeys(y.keys, x.keys) || lessKeys(x.keys, y.keys) {
		t.Fatal("order is not key by key")
	}
	// Folding: an unbounded family with two string keys, the second folding.
	si, err := compile(Schema{Families: []Family{{Name: "f", Keys: []Key{{Name: "a"}, {Name: "b", Fold: true}}, Measures: []string{"n"}, Cap: map[Grain]int{Hour: 5}}}})
	if err != nil {
		t.Fatal(err)
	}
	r := func(a, b string, n int64) rec { return rec{collector: "c", keys: []string{a, b}, vals: []int64{n}} }
	got := recSet(foldTop(si.byName["f"], []rec{r("a\x00b", "c", 3), r("a", "b\x00c", 3)}, 5))
	if len(got) != 2 {
		t.Fatalf("unbounded fold merged distinct tuples: %v", got)
	}
	// Bounded: two tied rows dropped at the cut stay apart by zone.
	bs, err := compile(boundSchema(1))
	if err != nil {
		t.Fatal(err)
	}
	b := func(z, n string, c int64) rec { return rec{collector: "c", keys: []string{z, n}, vals: []int64{c, 0}} }
	got = recSet(foldTop(bs.byName["names"], []rec{b("z", "top", 9), b("a\x00b", "c", 2), b("a", "b\x00c", 2)}, 1))
	if got["c|a\x00b|"+Other] == nil || got["c|a|"+Other] == nil {
		t.Fatalf("bounded fold merged distinct zones: %v", got)
	}
	// The same input in two orders gives the same rows.
	one := foldTop(si.byName["f"], []rec{r("a\x00b", "c", 3), r("a", "b\x00c", 3), r("a", "a", 3)}, 2)
	two := foldTop(si.byName["f"], []rec{r("a", "a", 3), r("a", "b\x00c", 3), r("a\x00b", "c", 3)}, 2)
	if fmt.Sprint(one) != fmt.Sprint(two) {
		t.Fatalf("order depends on input:\n%v\n%v", one, two)
	}
}
