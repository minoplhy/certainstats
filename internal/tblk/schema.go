package tblk

import (
	"fmt"
	"regexp"
	"slices"
	"time"
)

// Grain is a tier's bucket width.
type Grain int

const (
	Minute Grain = iota
	Hour
	Day
)

var grains = []Grain{Minute, Hour, Day}

const (
	minuteMs = int64(time.Minute / time.Millisecond)
	hourMs   = int64(time.Hour / time.Millisecond)
	dayMs    = 24 * hourMs
)

// Ms is the bucket width in milliseconds.
func (g Grain) Ms() int64 {
	switch g {
	case Hour:
		return hourMs
	case Day:
		return dayMs
	}
	return minuteMs
}

func (g Grain) String() string {
	switch g {
	case Hour:
		return "hour"
	case Day:
		return "day"
	}
	return "minute"
}

// KeyKind says how a key is stored and filtered.
type KeyKind int

const (
	// KeyString is any text value.
	KeyString KeyKind = iota
	// KeyNet is a network in CIDR form; filters take a prefix and match every
	// stored network inside it. It may also hold Other (a folded network),
	// which no prefix matches.
	KeyNet
	// KeyInt is a whole number, such as a size bucket.
	KeyInt
)

// Other is the value folded keys take.
const Other = "(other)"

// Key is one key column of a family.
type Key struct {
	Name string
	Kind KeyKind
	// Fold sets this key to Other when a row falls below the cap. Keys that
	// do not fold (a zone, a subnet) stay, so folded rows still split by them.
	Fold bool
	// Level orders folding keys that depend on each other (ip, then range,
	// then asn): below the cap a row loses its level-1 keys first and keeps
	// the rest; of those rows, the ones below the cap again lose level 2, and
	// so on. Zero means 1, so keys that fold together share one level.
	Level int
}

// Family is one set of rows with its own keys, measures and caps.
type Family struct {
	Name string
	Keys []Key
	// Measures are summed int64 columns. The first ranks rows for the cap.
	Measures []string
	// Cap is the rows kept per owner, collector and bucket when a tier is
	// rolled up; zero or missing keeps every row. Minute rows are stored as sent.
	Cap map[Grain]int
	// Bound names a measure that records how far capped rows can be off.
	// With it, a cap keeps only rows ranked strictly above the first row
	// past the cap, so rows tied at the cut drop together, and adds that
	// row's rank measure (the cut) to the bound row: the row whose keys are
	// all Other. When every key folds, that row also holds the folded rows'
	// sums. A key's true sum is then at least its stored sum and at
	// most that plus the bound rows' sum over the same range. Requires one
	// fold level and string or network keys only. Writers put the error of cuts made
	// before the data arrived (such as a collector's own) on the bound row
	// of each minute.
	Bound string
}

// BoundKeys is the key set of a bounded family's bound row: every key Other.
func BoundKeys(f Family) []string {
	out := make([]string, len(f.Keys))
	for i := range out {
		out[i] = Other
	}
	return out
}

// Schema is what a collector type declares.
type Schema struct {
	Families []Family
	// Retention per tier; zero keeps that tier forever.
	Retention map[Grain]time.Duration
	// MaxBytes caps the store's size; zero means no cap.
	MaxBytes int64
}

// DefaultRetention is the retention a type gets unless it sets its own.
var DefaultRetention = map[Grain]time.Duration{
	Minute: 7 * 24 * time.Hour,
	Hour:   90 * 24 * time.Hour,
	Day:    400 * 24 * time.Hour,
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// famInfo is a validated family with lookups.
type famInfo struct {
	Family
	idx    int
	keyIdx map[string]int
	msrIdx map[string]int
	// rank is the first level-1 folding key, or -1: a row is folded when it is Other.
	rank int
	// level is each key's fold level, 0 for keys that do not fold; levels
	// lists the folding keys of level 1, 2, ... in order.
	level  []int
	levels [][]int
	// bound is the Bound measure's index, or -1.
	bound int
}

// isBound reports whether keys are the bound row's (every key Other).
func (f *famInfo) isBound(keys []string) bool {
	if f.bound < 0 {
		return false
	}
	for _, k := range keys {
		if k != Other {
			return false
		}
	}
	return true
}

// foldedLevels counts the levels a row has already lost, from level 1 up.
func (f *famInfo) foldedLevels(keys []string) int {
	n := 0
	for _, lv := range f.levels {
		if keys[lv[0]] != Other {
			break
		}
		n++
	}
	return n
}

// foldTo returns keys with every folding key of levels 1 to n set to Other.
func (f *famInfo) foldTo(keys []string, n int) []string {
	out := append([]string(nil), keys...)
	for i := 0; i < n && i < len(f.levels); i++ {
		for _, k := range f.levels[i] {
			out[k] = Other
		}
	}
	return out
}

func (f *famInfo) cap(g Grain) int { return f.Cap[g] }

// folded reports whether a row's keys are a folded (Other) row.
func (f *famInfo) folded(keys []string) bool {
	return f.rank >= 0 && keys[f.rank] == Other
}

// foldKeys returns keys with every folding key set to Other.
func (f *famInfo) foldKeys(keys []string) []string { return f.foldTo(keys, len(f.levels)) }

type schemaInfo struct {
	Schema
	fams   []*famInfo
	byName map[string]*famInfo
}

func compile(s Schema) (*schemaInfo, error) {
	si := &schemaInfo{Schema: s, byName: map[string]*famInfo{}}
	if len(s.Families) == 0 {
		return nil, fmt.Errorf("tblk: schema has no families")
	}
	for i, f := range s.Families {
		if !nameRe.MatchString(f.Name) {
			return nil, fmt.Errorf("tblk: family name %q must be lower-case letters, digits and _", f.Name)
		}
		if si.byName[f.Name] != nil {
			return nil, fmt.Errorf("tblk: family %q declared twice", f.Name)
		}
		if len(f.Measures) == 0 {
			return nil, fmt.Errorf("tblk: family %q has no measures", f.Name)
		}
		fi := &famInfo{Family: f, idx: i, keyIdx: map[string]int{}, msrIdx: map[string]int{}, rank: -1, bound: -1}
		for j, k := range f.Keys {
			if !nameRe.MatchString(k.Name) {
				return nil, fmt.Errorf("tblk: key name %q in %q must be lower-case letters, digits and _", k.Name, f.Name)
			}
			if _, dup := fi.keyIdx[k.Name]; dup {
				return nil, fmt.Errorf("tblk: key %q declared twice in %q", k.Name, f.Name)
			}
			if k.Fold && k.Kind == KeyInt {
				return nil, fmt.Errorf("tblk: key %q in %q folds but is a number key", k.Name, f.Name)
			}
			if k.Level < 0 || (k.Level > 0 && !k.Fold) {
				return nil, fmt.Errorf("tblk: key %q in %q has level %d; levels are for folding keys and start at 1", k.Name, f.Name, k.Level)
			}
			lv := 0
			if k.Fold {
				lv = max(k.Level, 1)
			}
			fi.level = append(fi.level, lv)
			fi.keyIdx[k.Name] = j
		}
		// Levels in use, in order; a gap (1 and 3 without 2) is closed up.
		var used []int
		for _, lv := range fi.level {
			if lv > 0 && !slices.Contains(used, lv) {
				used = append(used, lv)
			}
		}
		slices.Sort(used)
		for _, lv := range used {
			var keys []int
			for j, l := range fi.level {
				if l == lv {
					keys = append(keys, j)
				}
			}
			fi.levels = append(fi.levels, keys)
		}
		for j, l := range fi.level {
			if l > 0 {
				fi.level[j] = slices.Index(used, l) + 1
			}
		}
		if len(fi.levels) > 0 {
			fi.rank = fi.levels[0][0]
		}
		for j, m := range f.Measures {
			if !nameRe.MatchString(m) {
				return nil, fmt.Errorf("tblk: measure name %q in %q must be lower-case letters, digits and _", m, f.Name)
			}
			if _, dup := fi.msrIdx[m]; dup {
				return nil, fmt.Errorf("tblk: measure %q declared twice in %q", m, f.Name)
			}
			fi.msrIdx[m] = j
		}
		if f.Bound != "" {
			b, ok := fi.msrIdx[f.Bound]
			switch {
			case !ok:
				return nil, fmt.Errorf("tblk: family %q bounds with measure %q, which it does not have", f.Name, f.Bound)
			case b == 0:
				return nil, fmt.Errorf("tblk: family %q cannot bound with its rank measure %q", f.Name, f.Bound)
			case len(fi.levels) != 1:
				return nil, fmt.Errorf("tblk: bounded family %q needs exactly one fold level", f.Name)
			}
			for _, k := range f.Keys {
				if k.Kind == KeyInt {
					return nil, fmt.Errorf("tblk: bounded family %q has key %q that is a number key", f.Name, k.Name)
				}
			}
			fi.bound = b
		}
		for g, n := range f.Cap {
			if n > 0 && fi.rank < 0 {
				return nil, fmt.Errorf("tblk: family %q caps the %s tier but has no folding key", f.Name, g)
			}
		}
		si.fams = append(si.fams, fi)
		si.byName[f.Name] = fi
	}
	if si.Retention == nil {
		si.Retention = DefaultRetention
	}
	return si, nil
}
