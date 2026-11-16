package knot

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"certainstats/internal/collector/type/knot/wire"
)

// statsSection is where Knot reports mod-stats counters: the module's name
// (stats_modules uses mod->id->name), whatever the module instance's id.
const statsSection = "mod-stats"

// parseCounters turns stats or zone-stats units into mod-stats counters. Other
// sections are not counters (server.zone-count, zone.size and zone.max-ttl are
// current values) and are dropped, as are values that are not whole numbers.
func parseCounters(units []unit) []wire.Counter {
	out := make([]wire.Counter, 0, len(units))
	for _, u := range units {
		if u.set[idxError] || u.get(idxSection) != statsSection || !u.set[idxItem] {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSpace(u.get(idxData)), 10, 64)
		if err != nil {
			continue
		}
		out = append(out, wire.Counter{
			Section: u.get(idxSection),
			Item:    u.get(idxItem),
			ID:      u.get(idxID),
			Zone:    canonicalZone(u.get(idxZone)),
			Value:   v,
		})
	}
	return out
}

// hasModStats reports whether any counter comes from mod-stats.
func hasModStats(cs []wire.Counter) bool {
	for _, c := range cs {
		if c.Section == statsSection {
			return true
		}
	}
	return false
}

// capZones keeps the counters of the max zones with the most requests, by
// mod-stats request-protocol (every request counts once there), and reports
// how many zones were left out.
func capZones(cs []wire.Counter, max int) ([]wire.Counter, int) {
	load := map[string]uint64{}
	for _, c := range cs {
		if _, ok := load[c.Zone]; !ok {
			load[c.Zone] = 0
		}
		if c.Item == "request-protocol" {
			load[c.Zone] += c.Value
		}
	}
	if len(load) <= max {
		return cs, 0
	}
	zones := make([]string, 0, len(load))
	for z := range load {
		zones = append(zones, z)
	}
	sort.Slice(zones, func(i, j int) bool {
		if load[zones[i]] != load[zones[j]] {
			return load[zones[i]] > load[zones[j]]
		}
		return zones[i] < zones[j]
	})
	keep := make(map[string]bool, max)
	for _, z := range zones[:max] {
		keep[z] = true
	}
	out := cs[:0]
	for _, c := range cs {
		if keep[c.Zone] {
			out = append(out, c)
		}
	}
	return out, len(zones) - max
}

// canonicalZone lowercases a zone name and ends it with a dot.
func canonicalZone(z string) string {
	z = strings.ToLower(strings.TrimSpace(z))
	if z == "" {
		return ""
	}
	if !strings.HasSuffix(z, ".") {
		z += "."
	}
	return z
}

// parseZones turns zone-status units into zones, in name order. Each unit
// carries one field (TYPE) and its value (DATA); times are relative to now.
func parseZones(units []unit, now time.Time) []wire.Zone {
	byName := map[string]*wire.Zone{}
	var order []string
	for _, u := range units {
		name := canonicalZone(u.get(idxZone))
		if name == "" {
			continue
		}
		z := byName[name]
		if z == nil {
			z = &wire.Zone{Name: name}
			byName[name] = z
			order = append(order, name)
		}
		if u.set[idxError] {
			z.Error = u.get(idxError)
			continue
		}
		applyZoneField(z, strings.ToLower(strings.TrimSpace(u.get(idxType))), strings.TrimSpace(u.get(idxData)), now)
	}
	sort.Strings(order)
	out := make([]wire.Zone, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}
	return out
}

func applyZoneField(z *wire.Zone, field, value string, now time.Time) {
	switch {
	case field == "role":
		switch strings.ToLower(value) {
		case "master", "primary":
			z.Role = "primary"
		case "slave", "secondary":
			z.Role = "secondary"
		}
	case field == "serial":
		if v, err := strconv.ParseUint(value, 10, 32); err == nil {
			z.Serial, z.HasSerial = uint32(v), true
		}
	case field == "freeze":
		// Knot: "yes" frozen, "thawing" frozen with a thaw scheduled,
		// "freezing" not frozen yet, "-" not frozen.
		z.Frozen = strings.EqualFold(value, "yes") || strings.EqualFold(value, "thawing")
	case field == "refresh":
		z.RefreshAtMs = eventTime(value, now)
	case field == "expiration":
		z.ExpireAtMs = eventTime(value, now)
	case strings.Contains(field, "re-sign") || strings.Contains(field, "resign"):
		z.ResignAtMs = eventTime(value, now)
	}
}

// eventTime reads a zone event time as Knot's zone-status prints it
// (knot/ctl/commands.c): a Unix time when asked with the "u" flag (Knot 3.3.3
// and later), or the relative form such as "+1D2h3m4s" from older versions,
// which ignore the flag. An overdue event is wrapped as "pending(<time>)" or
// "frozen(<time>)", a running one as "running(<start>)"; "-" means none.
func eventTime(v string, now time.Time) int64 {
	if i := strings.IndexByte(v, '('); i > 0 && strings.HasSuffix(v, ")") {
		v = v[i+1 : len(v)-1]
	}
	if v == "" || v == "-" {
		return 0
	}
	if v[0] == '+' || v[0] == '-' {
		d, ok := parseSpan(v[1:])
		if !ok {
			return 0
		}
		if v[0] == '-' {
			d = -d
		}
		return now.Add(d).UnixMilli()
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 1_000_000_000 {
		return n * 1000
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UnixMilli()
	}
	return 0
}

// spanUnits are Knot's human time units (libknot time.c).
var spanUnits = map[byte]time.Duration{
	'Y': 365 * 24 * time.Hour,
	'M': 30 * 24 * time.Hour,
	'D': 24 * time.Hour,
	'h': time.Hour,
	'm': time.Minute,
	's': time.Second,
}

// parseSpan reads "1D2h3m4s"; a bare number is seconds.
func parseSpan(s string) (time.Duration, bool) {
	if s == "" {
		return 0, false
	}
	var total time.Duration
	num := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			if num < 0 {
				num = 0
			}
			num = num*10 + int(c-'0')
			if num > 1_000_000_000 {
				return 0, false
			}
			continue
		}
		unit, ok := spanUnits[c]
		if !ok || num < 0 {
			return 0, false
		}
		total += time.Duration(num) * unit
		num = -1
	}
	if num >= 0 {
		total += time.Duration(num) * time.Second
	}
	return total, true
}
