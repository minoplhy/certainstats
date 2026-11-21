package knot

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"certainstats/internal/collector/type/knot/wire"

	"golang.org/x/net/dns/dnsmessage"
	"google.golang.org/protobuf/encoding/protowire"
)

// dnstap protobuf field numbers (dnstap.proto).
const (
	dtFieldMessage = 14
	dtFieldType    = 15
	dtTypeMessage  = 1

	msgFieldType      = 1
	msgFieldQueryAddr = 4
	msgFieldQueryMsg  = 10
	msgFieldResponse  = 14
	msgAuthQuery      = 1
	msgAuthResponse   = 2
	msgClientQuery    = 5
	msgClientResponse = 6
)

// event is the part of one dnstap message the aggregates need.
type event struct {
	response bool
	qname    string
	qtype    string
	rcode    string
	size     int
	client   net.IP
}

var errSkip = errors.New("not a query or response")

// decodeDnstap reads one dnstap frame. Messages other than authoritative or
// client queries and responses are skipped.
func decodeDnstap(frame []byte) (event, error) {
	var ev event
	var msg []byte
	isMessage := false
	for b := frame; len(b) > 0; {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return ev, protowire.ParseError(n)
		}
		b = b[n:]
		switch {
		case num == dtFieldType && typ == protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return ev, protowire.ParseError(m)
			}
			isMessage = v == dtTypeMessage
			b = b[m:]
		case num == dtFieldMessage && typ == protowire.BytesType:
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return ev, protowire.ParseError(m)
			}
			msg = v
			b = b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, b)
			if m < 0 {
				return ev, protowire.ParseError(m)
			}
			b = b[m:]
		}
	}
	if !isMessage || msg == nil {
		return ev, errSkip
	}
	var kind uint64
	var query, response, addr []byte
	for b := msg; len(b) > 0; {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return ev, protowire.ParseError(n)
		}
		b = b[n:]
		switch {
		case num == msgFieldType && typ == protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return ev, protowire.ParseError(m)
			}
			kind = v
			b = b[m:]
		case typ == protowire.BytesType && (num == msgFieldQueryAddr || num == msgFieldQueryMsg || num == msgFieldResponse):
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return ev, protowire.ParseError(m)
			}
			switch num {
			case msgFieldQueryAddr:
				addr = v
			case msgFieldQueryMsg:
				query = v
			case msgFieldResponse:
				response = v
			}
			b = b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, b)
			if m < 0 {
				return ev, protowire.ParseError(m)
			}
			b = b[m:]
		}
	}
	var dns []byte
	switch kind {
	case msgAuthQuery, msgClientQuery:
		dns = query
	case msgAuthResponse, msgClientResponse:
		ev.response, dns = true, response
	default:
		return ev, errSkip
	}
	if dns == nil {
		return ev, errors.New("dnstap message without its DNS message")
	}
	if len(addr) == 4 || len(addr) == 16 {
		ev.client = net.IP(addr)
	}
	var p dnsmessage.Parser
	h, err := p.Start(dns)
	if err != nil {
		return ev, err
	}
	q, err := p.Question()
	if err != nil {
		return ev, err
	}
	ev.qname = normalizeName(q.Name.String())
	ev.qtype = qtypeName(uint16(q.Type))
	if ev.response {
		ev.rcode = rcodeName(int(h.RCode))
		ev.size = len(dns)
	}
	return ev, nil
}

// normalizeName lowercases a name, ends it with a dot and caps it at 255 bytes.
func normalizeName(n string) string {
	n = strings.ToLower(n)
	if !strings.HasSuffix(n, ".") {
		n += "."
	}
	if len(n) > 255 {
		n = n[:255]
	}
	return n
}

// qtypeNames match the names Knot's mod-stats uses.
var qtypeNames = map[uint16]string{
	1: "A", 2: "NS", 5: "CNAME", 6: "SOA", 12: "PTR", 13: "HINFO", 15: "MX", 16: "TXT",
	28: "AAAA", 29: "LOC", 33: "SRV", 35: "NAPTR", 39: "DNAME", 43: "DS", 46: "RRSIG",
	47: "NSEC", 48: "DNSKEY", 50: "NSEC3", 51: "NSEC3PARAM", 52: "TLSA", 59: "CDS",
	60: "CDNSKEY", 64: "SVCB", 65: "HTTPS", 99: "SPF", 251: "IXFR", 252: "AXFR",
	255: "ANY", 257: "CAA",
}

func qtypeName(t uint16) string {
	if n, ok := qtypeNames[t]; ok {
		return n
	}
	return "TYPE" + strconv.Itoa(int(t))
}

var rcodeNames = []string{"NOERROR", "FORMERR", "SERVFAIL", "NXDOMAIN", "NOTIMPL", "REFUSED",
	"YXDOMAIN", "YXRRSET", "NXRRSET", "NOTAUTH", "NOTZONE"}

func rcodeName(c int) string {
	if c >= 0 && c < len(rcodeNames) {
		return rcodeNames[c]
	}
	return "RCODE" + strconv.Itoa(c)
}

// subnet is a client's /24 (IPv4) or /48 (IPv6).
func subnet(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String() + "/24"
	}
	return ip.Mask(net.CIDRMask(48, 128)).String() + "/48"
}

// zoneSet maps a name to the longest zone it falls in.
type zoneSet map[string]bool

func (zs zoneSet) zoneOf(name string) string {
	for n := name; ; {
		if zs[n] {
			return n
		}
		if n == "." || n == "" {
			return wire.NoZone
		}
		i := strings.IndexByte(n, '.')
		if i < 0 || i == len(n)-1 {
			n = "."
		} else {
			n = n[i+1:]
		}
	}
}

type totalKey struct{ zone, qtype, rcode string }
type nameKey struct{ zone, qname, qtype, rcode string }
type netNameKey struct{ subnet, zone, qname string }
type netCodeKey struct{ subnet, zone, qtype, rcode string }

// mapCap bounds each per-minute map, so a random-subdomain flood cannot grow
// memory without limit; past it, new keys fold into the zone's Other row.
const mapCap = 50_000

// minute holds one minute of aggregates.
type minute struct {
	start       int64
	totals      map[totalKey]uint64
	names       map[nameKey]uint64
	subnets     map[string]uint64 // each client network's traffic
	netNames    map[netNameKey]uint64
	netCodes    map[netCodeKey]uint64
	sizes       []uint64
	queriesOnly bool
	// overflow counts queries whose name row went to Other because the map
	// was full; a name can be that many short, so it adds to both cuts' errors.
	overflow uint64
	// netOverflow counts queries from networks past the map's cap, counted
	// under the Other network; netNameOverflow and netCodeOverflow count rows
	// folded early in their maps. Each adds to the matching lists' errors.
	netOverflow, netNameOverflow, netCodeOverflow uint64
}

func newMinute(start int64) *minute {
	return &minute{start: start, totals: map[totalKey]uint64{}, names: map[nameKey]uint64{},
		subnets: map[string]uint64{}, netNames: map[netNameKey]uint64{}, netCodes: map[netCodeKey]uint64{}, sizes: make([]uint64, len(wire.SizeBuckets)+1)}
}

// aggregator folds dnstap events into closed one-minute buckets.
type aggregator struct {
	mu          sync.Mutex
	zones       zoneSet
	clients     bool
	cur         *minute
	closed      []*minute
	sawResponse bool
}

func (a *aggregator) setZones(zs zoneSet) {
	a.mu.Lock()
	a.zones = zs
	a.mu.Unlock()
}

// add counts one event at its arrival time. Once Knot is seen logging
// responses, queries are ignored: each response already stands for its query.
func (a *aggregator) add(ev event, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ev.response {
		a.sawResponse = true
	} else if a.sawResponse {
		return
	}
	start := now.Truncate(time.Minute).UnixMilli()
	if a.cur == nil || a.cur.start != start {
		if a.cur != nil {
			a.closed = append(a.closed, a.cur)
		}
		a.cur = newMinute(start)
	}
	m := a.cur
	if !ev.response {
		m.queriesOnly = true
	}
	zone := a.zones.zoneOf(ev.qname)
	tk := totalKey{zone, ev.qtype, ev.rcode}
	if _, ok := m.totals[tk]; !ok && len(m.totals) >= mapCap {
		tk.qtype = "other"
	}
	m.totals[tk]++
	nk := nameKey{zone, ev.qname, ev.qtype, ev.rcode}
	if _, ok := m.names[nk]; !ok && len(m.names) >= mapCap {
		nk = nameKey{zone, wire.Other, tk.qtype, ev.rcode}
		m.overflow++
	}
	m.names[nk]++
	if a.clients && ev.client != nil {
		s := subnet(ev.client)
		if _, ok := m.subnets[s]; !ok && len(m.subnets) >= mapCap {
			s = wire.Other
			m.netOverflow++
		}
		m.subnets[s]++
		nn := netNameKey{s, zone, ev.qname}
		if _, ok := m.netNames[nn]; !ok && len(m.netNames) >= mapCap {
			nn.qname = wire.Other
			m.netNameOverflow++
		}
		m.netNames[nn]++
		nc := netCodeKey{s, zone, tk.qtype, ev.rcode}
		if _, ok := m.netCodes[nc]; !ok && len(m.netCodes) >= mapCap {
			nc.qtype, nc.rcode = wire.Other, wire.Other
			m.netCodeOverflow++
		}
		m.netCodes[nc]++
	}
	if ev.response {
		i := sort.SearchInts(wire.SizeBuckets, ev.size)
		m.sizes[i]++
	}
}

// take returns every minute that has ended, oldest first.
func (a *aggregator) take(now time.Time) []*minute {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cur != nil && now.UnixMilli() >= a.cur.start+time.Minute.Milliseconds() {
		a.closed = append(a.closed, a.cur)
		a.cur = nil
	}
	out := a.closed
	a.closed = nil
	return out
}

// bucket turns a minute into its wire form. Name rows and per-name counts are
// each cut with the bound rule (wire.Cut): entries above the cut stay, the rest,
// ties at the cut included, fold into Other rows (per zone, qtype and rcode for
// rows; per zone for names), so sums still match totals. The cuts, plus any
// queries folded early because the map was full, are sent as the errors.
// Client networks are cut the same way: per network at top, and network by
// name and network by qtype and rcode at clientTop (see netLists).
func (m *minute) bucket(top, clientTop int) wire.Dnstap {
	d := wire.Dnstap{MinuteMs: m.start, QueriesOnly: m.queriesOnly, Bounded: true}
	for k, n := range m.totals {
		d.Totals = append(d.Totals, wire.Total{Zone: k.zone, QType: k.qtype, RCode: k.rcode, Count: n})
	}
	sort.Slice(d.Totals, func(i, j int) bool { return lessTotal(d.Totals[i], d.Totals[j]) })
	rows := make([]wire.Name, 0, len(m.names))
	other := map[nameKey]uint64{}
	byName := map[[2]string]uint64{}
	nameOther := map[string]uint64{}
	for k, n := range m.names {
		if k.qname == wire.Other {
			other[k] += n
			nameOther[k.zone] += n
			continue
		}
		rows = append(rows, wire.Name{Zone: k.zone, QName: k.qname, QType: k.qtype, RCode: k.rcode, Count: n})
		byName[[2]string{k.zone, k.qname}] += n
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return lessName(rows[i], rows[j])
	})
	counts := make([]uint64, len(rows))
	for i, r := range rows {
		counts[i] = r.Count
	}
	cut := wire.Cut(counts, top)
	kept := rows[:0]
	for _, r := range rows {
		if r.Count > cut {
			kept = append(kept, r)
			continue
		}
		other[nameKey{r.Zone, wire.Other, r.QType, r.RCode}] += r.Count
	}
	rows = kept
	for k, n := range other {
		rows = append(rows, wire.Name{Zone: k.zone, QName: wire.Other, QType: k.qtype, RCode: k.rcode, Count: n})
	}
	d.Names = rows
	d.NameErr = cut + m.overflow
	d.QNames, d.QNameErr = qnameRows(byName, nameOther, top)
	d.QNameErr += m.overflow
	m.netLists(&d, top, clientTop)
	for i, n := range m.sizes {
		if n == 0 {
			continue
		}
		upTo := 0
		if i < len(wire.SizeBuckets) {
			upTo = wire.SizeBuckets[i]
		}
		d.Sizes = append(d.Sizes, wire.Size{UpTo: upTo, Count: n})
	}
	return d
}

// qnameRows cuts per-name counts with the bound rule: names above the cut
// stay, the rest join their zone's Other row. It returns the rows and the cut.
func qnameRows(byName map[[2]string]uint64, other map[string]uint64, top int) ([]wire.QName, uint64) {
	rows := make([]wire.QName, 0, len(byName))
	for k, n := range byName {
		rows = append(rows, wire.QName{Zone: k[0], QName: k[1], Count: n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		if rows[i].Zone != rows[j].Zone {
			return rows[i].Zone < rows[j].Zone
		}
		return rows[i].QName < rows[j].QName
	})
	counts := make([]uint64, len(rows))
	for i, r := range rows {
		counts[i] = r.Count
	}
	cut := wire.Cut(counts, top)
	folded := map[string]uint64{}
	for z, n := range other {
		folded[z] += n
	}
	kept := rows[:0]
	for _, r := range rows {
		if r.Count > cut {
			kept = append(kept, r)
			continue
		}
		folded[r.Zone] += r.Count
	}
	zones := make([]string, 0, len(folded))
	for z := range folded {
		zones = append(zones, z)
	}
	sort.Strings(zones)
	for _, z := range zones {
		kept = append(kept, wire.QName{Zone: z, QName: wire.Other, Count: folded[z]})
	}
	return kept, cut
}

// cutCounts sorts rows largest first (ties by less, for a stable order) and
// splits them with the bound rule: rows above the cut, the rest, and the cut.
func cutCounts[T any](rows []T, count func(T) uint64, less func(a, b T) bool, top int) (kept, dropped []T, cut uint64) {
	sort.Slice(rows, func(i, j int) bool {
		if a, b := count(rows[i]), count(rows[j]); a != b {
			return a > b
		}
		return less(rows[i], rows[j])
	})
	counts := make([]uint64, len(rows))
	for i, r := range rows {
		counts[i] = count(r)
	}
	cut = wire.Cut(counts, top)
	for i := range rows {
		if counts[i] <= cut {
			return rows[:i], rows[i:], cut
		}
	}
	return rows, nil, cut
}

// netLists cuts the client networks with the bound rule into three lists:
// networks at top, with one Other network for the rest; network by name and
// network by qtype and rcode at clientTop, each folding into one row per
// zone with the network and the other keys Other (a row per network would
// grow with every network seen). Every list adds up to the client traffic, and each error
// is its cut plus the queries its maps folded early.
func (m *minute) netLists(d *wire.Dnstap, top, clientTop int) {
	if len(m.subnets) == 0 {
		return
	}
	nets := make([]wire.Net, 0, len(m.subnets))
	otherNet := m.subnets[wire.Other]
	for s, n := range m.subnets {
		if s != wire.Other {
			nets = append(nets, wire.Net{Subnet: s, Count: n})
		}
	}
	kept, dropped, cut := cutCounts(nets, func(r wire.Net) uint64 { return r.Count }, func(a, b wire.Net) bool { return a.Subnet < b.Subnet }, top)
	for _, r := range dropped {
		otherNet += r.Count
	}
	if otherNet > 0 {
		kept = append(kept, wire.Net{Subnet: wire.Other, Count: otherNet})
	}
	d.Nets, d.NetErr = kept, cut+m.netOverflow

	names := make([]wire.NetName, 0, len(m.netNames))
	nameOther := map[string]uint64{}
	for k, n := range m.netNames {
		if k.qname == wire.Other || k.subnet == wire.Other {
			nameOther[k.zone] += n
			continue
		}
		names = append(names, wire.NetName{Subnet: k.subnet, Zone: k.zone, QName: k.qname, Count: n})
	}
	keptN, droppedN, cutN := cutCounts(names, func(r wire.NetName) uint64 { return r.Count }, func(a, b wire.NetName) bool {
		if a.Subnet != b.Subnet {
			return a.Subnet < b.Subnet
		}
		if a.Zone != b.Zone {
			return a.Zone < b.Zone
		}
		return a.QName < b.QName
	}, clientTop)
	for _, r := range droppedN {
		nameOther[r.Zone] += r.Count
	}
	for _, z := range sortedKeys(nameOther) {
		keptN = append(keptN, wire.NetName{Subnet: wire.Other, Zone: z, QName: wire.Other, Count: nameOther[z]})
	}
	d.NetNames, d.NetNameErr = keptN, cutN+m.netNameOverflow+m.netOverflow

	codes := make([]wire.NetCode, 0, len(m.netCodes))
	codeOther := map[string]uint64{}
	for k, n := range m.netCodes {
		if (k.qtype == wire.Other && k.rcode == wire.Other) || k.subnet == wire.Other {
			codeOther[k.zone] += n
			continue
		}
		codes = append(codes, wire.NetCode{Subnet: k.subnet, Zone: k.zone, QType: k.qtype, RCode: k.rcode, Count: n})
	}
	keptC, droppedC, cutC := cutCounts(codes, func(r wire.NetCode) uint64 { return r.Count }, func(a, b wire.NetCode) bool {
		if a.Subnet != b.Subnet {
			return a.Subnet < b.Subnet
		}
		if a.Zone != b.Zone {
			return a.Zone < b.Zone
		}
		if a.QType != b.QType {
			return a.QType < b.QType
		}
		return a.RCode < b.RCode
	}, clientTop)
	for _, r := range droppedC {
		codeOther[r.Zone] += r.Count
	}
	for _, z := range sortedKeys(codeOther) {
		keptC = append(keptC, wire.NetCode{Subnet: wire.Other, Zone: z, QType: wire.Other, RCode: wire.Other, Count: codeOther[z]})
	}
	d.NetCodes, d.NetCodeErr = keptC, cutC+m.netCodeOverflow+m.netOverflow
}

func sortedKeys(m map[string]uint64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func lessTotal(a, b wire.Total) bool {
	if a.Zone != b.Zone {
		return a.Zone < b.Zone
	}
	if a.QType != b.QType {
		return a.QType < b.QType
	}
	return a.RCode < b.RCode
}

func lessName(a, b wire.Name) bool {
	if a.Zone != b.Zone {
		return a.Zone < b.Zone
	}
	if a.QName != b.QName {
		return a.QName < b.QName
	}
	if a.QType != b.QType {
		return a.QType < b.QType
	}
	return a.RCode < b.RCode
}

// split cuts a bucket into parts that each encode within budget bytes. Rows are
// disjoint across parts; sizes ride in the first part, and the bound fields
// in every part.
func split(d wire.Dnstap, budget int) []wire.Dnstap {
	const rowOverhead = 24
	head := func() wire.Dnstap {
		return wire.Dnstap{MinuteMs: d.MinuteMs, QueriesOnly: d.QueriesOnly, Bounded: d.Bounded, QNameErr: d.QNameErr, NameErr: d.NameErr,
			NetErr: d.NetErr, NetNameErr: d.NetNameErr, NetCodeErr: d.NetCodeErr}
	}
	first := head()
	first.Sizes = d.Sizes
	used := 64
	parts := []wire.Dnstap{first}
	cur := &parts[0]
	next := func(n int) {
		if used+n > budget && rows(cur) > 0 {
			parts = append(parts, head())
			cur = &parts[len(parts)-1]
			used = 64
		}
		used += n
	}
	for _, t := range d.Totals {
		next(len(t.Zone) + len(t.QType) + len(t.RCode) + rowOverhead)
		cur.Totals = append(cur.Totals, t)
	}
	for _, n := range d.Names {
		next(len(n.Zone) + len(n.QName) + len(n.QType) + len(n.RCode) + rowOverhead)
		cur.Names = append(cur.Names, n)
	}
	for _, q := range d.QNames {
		next(len(q.Zone) + len(q.QName) + rowOverhead)
		cur.QNames = append(cur.QNames, q)
	}
	for _, c := range d.Nets {
		next(len(c.Subnet) + rowOverhead)
		cur.Nets = append(cur.Nets, c)
	}
	for _, c := range d.NetNames {
		next(len(c.Subnet) + len(c.Zone) + len(c.QName) + rowOverhead)
		cur.NetNames = append(cur.NetNames, c)
	}
	for _, c := range d.NetCodes {
		next(len(c.Subnet) + len(c.Zone) + len(c.QType) + len(c.RCode) + rowOverhead)
		cur.NetCodes = append(cur.NetCodes, c)
	}
	if len(parts) > 1 {
		for i := range parts {
			parts[i].Part, parts[i].Parts = i, len(parts)
		}
	}
	return parts
}

// rows counts a part's rows.
func rows(d *wire.Dnstap) int {
	return len(d.Totals) + len(d.Names) + len(d.QNames) + len(d.Nets) + len(d.NetNames) + len(d.NetCodes)
}

// errText keeps a listener error short for the status line.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
