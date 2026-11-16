package knot

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"certainstats/internal/knot/wire"

	"golang.org/x/net/dns/dnsmessage"
	"google.golang.org/protobuf/encoding/protowire"
)

// fakeKnot answers control requests on a unix socket the way knotd does.
type fakeKnot struct {
	path     string
	conns    atomic.Int32
	mu       sync.Mutex
	commands []string
	flags    []string
	replies  map[string][]map[int]string // command → units
}

func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "kn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func startFakeKnot(t *testing.T, replies map[string][]map[int]string) *fakeKnot {
	t.Helper()
	f := &fakeKnot{path: filepath.Join(shortDir(t), "knot.sock"), replies: replies}
	ln, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.conns.Add(1)
			go f.serve(c)
		}
	}()
	return f
}

func encodeUnit(typ byte, items map[int]string) []byte {
	buf := []byte{typ}
	for i := 0; i < idxCount; i++ {
		if v, ok := items[i]; ok {
			buf = append(buf, byte(dataOffset+i))
			buf = binary.BigEndian.AppendUint16(buf, uint16(len(v)))
			buf = append(buf, v...)
		}
	}
	return buf
}

func (f *fakeKnot) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	units, err := readReply(r) // a request has the same shape as a reply
	if err != nil || len(units) == 0 {
		return
	}
	cmd := units[0].get(idxCmd)
	f.mu.Lock()
	f.commands = append(f.commands, cmd)
	f.flags = append(f.flags, units[0].get(idxFlags))
	f.mu.Unlock()
	var out []byte
	for i, u := range f.replies[cmd] {
		// Like knotd: the first unit is DATA, the rest EXTRA, errors always DATA.
		typ := typeData
		if _, isErr := u[idxError]; i > 0 && !isErr {
			typ = typeExtra
		}
		out = append(out, encodeUnit(typ, u)...)
	}
	out = append(out, typeBlock)
	c.Write(out)
	io.Copy(io.Discard, r) // wait for END and close
}

// The replies below follow knotd's ctl code: zone_status() and ctl_dump_ctr()
// in knot/ctl/commands.c, and ctl_send_error(), which copies the request.
func TestControlProtocolAndParsing(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	unix := func(d time.Duration) string { return strconv.FormatInt(now.Add(d).Unix(), 10) }
	f := startFakeKnot(t, map[string][]map[int]string{
		"status": {{idxData: "3.4.2"}},
		"stats": {
			{idxSection: "server", idxItem: "zone-count", idxData: "2"}, // a current value, not a counter
			{idxSection: "mod-stats", idxItem: "request-protocol", idxID: "udp4", idxData: "120"},
			{idxSection: "mod-stats", idxItem: "response-code", idxID: "NXDOMAIN", idxData: "7"},
			{idxSection: "mod-stats", idxItem: "response-code", idxID: "NOERROR", idxData: "not-a-number"},
		},
		"zone-stats": {
			{idxZone: "example.com.", idxSection: "zone", idxItem: "size", idxData: "4096"},
			{idxZone: "example.com.", idxSection: "mod-stats", idxItem: "request-protocol", idxID: "udp6", idxData: "9"},
		},
		"zone-status": {
			{idxZone: "Example.com.", idxType: "role", idxData: "master"},
			{idxZone: "Example.com.", idxType: "serial", idxData: "2026111601"},
			{idxZone: "example.com.", idxType: "freeze", idxData: "freezing"},
			{idxZone: "example.com.", idxType: "refresh", idxData: "-"},
			{idxZone: "example.com.", idxType: "re-sign", idxData: "running(" + unix(-time.Second) + ")"},
			{idxZone: "example.com.", idxType: "DNSSEC-validate", idxData: unix(time.Hour)},
			{idxZone: "example.net.", idxType: "role", idxData: "slave"},
			{idxZone: "example.net.", idxType: "serial", idxData: "41"},
			{idxZone: "example.net.", idxType: "freeze", idxData: "thawing"},
			{idxZone: "example.net.", idxType: "refresh", idxData: "pending(" + unix(-5*time.Minute) + ")"},
			{idxZone: "example.net.", idxType: "expiration", idxData: unix(7*24*time.Hour - time.Second)},
			{idxZone: "older.org.", idxType: "role", idxData: "slave"},
			{idxZone: "older.org.", idxType: "serial", idxData: "-"},
			{idxZone: "older.org.", idxType: "expiration", idxData: "+6D23h59m59s"}, // before Knot 3.3.3
			{idxZone: "older.org.", idxType: "refresh", idxData: "frozen(-1h)"},
			// Some zone failed: knotd sends the good zones, then a command-wide error.
			{idxCmd: "zone-status", idxFlags: "u", idxError: "operation failed for some zones"},
		},
	})
	c := &ctl{path: f.path, timeout: 2 * time.Second}
	ctx := context.Background()

	units, err := c.call(ctx, request{idxCmd: "status", idxType: "version"})
	if err != nil || units[0].get(idxData) != "3.4.2" {
		t.Fatalf("status: %v %v", units, err)
	}
	units, err = c.call(ctx, request{idxCmd: "stats", idxFlags: "F"})
	if err != nil {
		t.Fatal(err)
	}
	cs := parseCounters(units)
	if len(cs) != 2 || !hasModStats(cs) || cs[0] != (wire.Counter{Section: "mod-stats", Item: "request-protocol", ID: "udp4", Value: 120}) {
		t.Fatalf("counters: %+v", cs)
	}
	units, err = c.call(ctx, request{idxCmd: "zone-stats"})
	if zc := parseCounters(units); err != nil || len(zc) != 1 || zc[0].Zone != "example.com." || zc[0].Value != 9 {
		t.Fatalf("zone counters must skip zone.size: %+v %v", zc, err)
	}

	units, err = c.call(ctx, request{idxCmd: "zone-status", idxFlags: "u"})
	if !partial(units, err) {
		t.Fatalf("a trailing zone error must keep the zones: %v", err)
	}
	zones := parseZones(units, now)
	if len(zones) != 3 {
		t.Fatalf("zones: %+v", zones)
	}
	if z := zones[0]; z.Name != "example.com." || z.Role != "primary" || !z.HasSerial || z.Serial != 2026111601 ||
		z.RefreshAtMs != 0 || z.Frozen || z.ResignAtMs != now.Add(-time.Second).UnixMilli() {
		t.Fatalf("primary: %+v", z)
	}
	if z := zones[1]; z.Role != "secondary" || z.Serial != 41 || !z.Frozen || z.RefreshAtMs != now.Add(-5*time.Minute).UnixMilli() ||
		z.ExpireAtMs != now.Add(7*24*time.Hour-time.Second).UnixMilli() {
		t.Fatalf("secondary: %+v", z)
	}
	if z := zones[2]; z.HasSerial || z.ExpireAtMs != now.Add(7*24*time.Hour-time.Second).UnixMilli() || z.RefreshAtMs != now.Add(-time.Hour).UnixMilli() {
		t.Fatalf("older Knot: %+v", z)
	}
}

func TestOnlyReadCommandsReachKnot(t *testing.T) {
	f := startFakeKnot(t, nil)
	c := &ctl{path: f.path}
	for _, cmd := range []string{"stop", "reload", "zone-purge", "zone-reload", "conf-set", "zone-freeze", ""} {
		if _, err := c.call(context.Background(), request{idxCmd: cmd}); err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Fatalf("%q: %v", cmd, err)
		}
	}
	if n := f.conns.Load(); n != 0 {
		t.Fatalf("a refused command still opened %d connections", n)
	}
	for cmd := range allowed {
		if strings.Contains(cmd, "purge") || strings.Contains(cmd, "set") || cmd == "stop" || cmd == "reload" {
			t.Fatalf("allowed list holds a write command: %s", cmd)
		}
	}
}

func TestCommandErrorIsReported(t *testing.T) {
	f := startFakeKnot(t, map[string][]map[int]string{"stats": {{idxError: "operation not permitted"}}})
	_, err := (&ctl{path: f.path}).call(context.Background(), request{idxCmd: "stats"})
	var ce *ctlError
	if !errors.As(err, &ce) || ce.msg != "operation not permitted" {
		t.Fatalf("got %v", err)
	}
}

func TestEventTimes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cases := map[string]int64{
		"+1D2h3m4s":     now.Add(26*time.Hour + 3*time.Minute + 4*time.Second).UnixMilli(),
		"-30s":          now.Add(-30 * time.Second).UnixMilli(),
		"+45":           now.Add(45 * time.Second).UnixMilli(),
		"+1Y":           now.Add(365 * 24 * time.Hour).UnixMilli(),
		"not scheduled": 0, "running": 0, "pending": 0, "-": 0, "+": 0, "+1x": 0,
		"1900000000":           1_900_000_000_000,
		"2027-01-02T03:04:05Z": time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC).UnixMilli(),
	}
	for in, want := range cases {
		if got := eventTime(in, now); got != want {
			t.Errorf("%q: got %d, want %d", in, got, want)
		}
	}
}

func TestZoneStatsCapKeepsBusiestZones(t *testing.T) {
	var cs []wire.Counter
	for i, z := range []string{"a.", "b.", "c.", "d."} {
		cs = append(cs,
			wire.Counter{Section: "mod-stats", Item: "request-protocol", ID: "udp4", Zone: z, Value: uint64(10 * (i + 1))},
			wire.Counter{Section: "mod-stats", Item: "response-code", ID: "NOERROR", Zone: z, Value: 1})
	}
	kept, dropped := capZones(cs, 2)
	if dropped != 2 || len(kept) != 4 {
		t.Fatalf("kept %d, dropped %d", len(kept), dropped)
	}
	for _, c := range kept {
		if c.Zone != "c." && c.Zone != "d." {
			t.Fatalf("kept a quiet zone: %+v", c)
		}
	}
	if _, d := capZones(cs, 10); d != 0 {
		t.Fatal("cap dropped zones under the limit")
	}
}

// dnstapFrame builds one dnstap protobuf frame for a query or response.
func dnstapFrame(t *testing.T, response bool, name string, qtype dnsmessage.Type, rcode dnsmessage.RCode, client net.IP) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: response, RCode: rcode})
	b.StartQuestions()
	if err := b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(name), Type: qtype, Class: dnsmessage.ClassINET}); err != nil {
		t.Fatal(err)
	}
	dns, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	var msg []byte
	kind, field := uint64(msgAuthQuery), protowire.Number(msgFieldQueryMsg)
	if response {
		kind, field = msgAuthResponse, msgFieldResponse
	}
	msg = protowire.AppendTag(msg, msgFieldType, protowire.VarintType)
	msg = protowire.AppendVarint(msg, kind)
	msg = protowire.AppendTag(msg, msgFieldQueryAddr, protowire.BytesType)
	msg = protowire.AppendBytes(msg, client.To4())
	msg = protowire.AppendTag(msg, 6, protowire.VarintType) // query_port, skipped
	msg = protowire.AppendVarint(msg, 5353)
	msg = protowire.AppendTag(msg, field, protowire.BytesType)
	msg = protowire.AppendBytes(msg, dns)
	var frame []byte
	frame = protowire.AppendTag(frame, 1, protowire.BytesType) // identity, skipped
	frame = protowire.AppendBytes(frame, []byte("knot"))
	frame = protowire.AppendTag(frame, dtFieldType, protowire.VarintType)
	frame = protowire.AppendVarint(frame, dtTypeMessage)
	frame = protowire.AppendTag(frame, dtFieldMessage, protowire.BytesType)
	frame = protowire.AppendBytes(frame, msg)
	return frame
}

func TestDnstapStreamAndAggregation(t *testing.T) {
	agg := &aggregator{zones: zoneSet{"example.com.": true, "sub.example.com.": true}, clients: true}
	now := time.Date(2026, 11, 16, 12, 0, 30, 0, time.UTC)
	writer, reader := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveStream(reader, func(f []byte) {
			ev, err := decodeDnstap(f)
			if err == nil {
				agg.add(ev, now)
			}
		})
	}()
	w := bufio.NewReader(writer)
	if err := writeControl(writer, ctrlReady, dnstapContentType); err != nil {
		t.Fatal(err)
	}
	if _, c, err := readFrame(w); err != nil || c == nil || c.typ != ctrlAccept {
		t.Fatalf("expected ACCEPT, got %+v %v", c, err)
	}
	if err := writeControl(writer, ctrlStart, dnstapContentType); err != nil {
		t.Fatal(err)
	}
	send := func(frame []byte) {
		b := binary.BigEndian.AppendUint32(nil, uint32(len(frame)))
		if _, err := writer.Write(append(b, frame...)); err != nil {
			t.Fatal(err)
		}
	}
	ip := net.ParseIP("192.0.2.77")
	// A query before any response is counted until responses show up; then queries are ignored.
	send(dnstapFrame(t, false, "early.example.com.", dnsmessage.TypeA, 0, ip))
	for i := 0; i < 3; i++ {
		send(dnstapFrame(t, true, "WWW.Example.com.", dnsmessage.TypeAAAA, dnsmessage.RCodeSuccess, ip))
	}
	send(dnstapFrame(t, false, "www.example.com.", dnsmessage.TypeAAAA, 0, ip))
	send(dnstapFrame(t, true, "a.sub.example.com.", dnsmessage.TypeA, dnsmessage.RCodeNameError, ip))
	send(dnstapFrame(t, true, "nothere.org.", dnsmessage.TypeTXT, dnsmessage.RCodeRefused, ip))
	send([]byte{0xff, 0xff}) // garbage is dropped, not fatal
	if err := writeControl(writer, ctrlStop, ""); err != nil {
		t.Fatal(err)
	}
	if _, c, err := readFrame(w); err != nil || c == nil || c.typ != ctrlFinish {
		t.Fatalf("expected FINISH, got %+v %v", c, err)
	}
	if err := <-done; !errors.Is(err, errStopped) {
		t.Fatalf("stream ended with %v", err)
	}

	mins := agg.take(now.Add(time.Minute))
	if len(mins) != 1 {
		t.Fatalf("minutes: %d", len(mins))
	}
	d := mins[0].bucket(1, 10)
	if d.MinuteMs != time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC).UnixMilli() || !d.QueriesOnly {
		t.Fatalf("bucket head: %+v", d)
	}
	totals := map[totalKey]uint64{}
	var sum uint64
	for _, x := range d.Totals {
		totals[totalKey{x.Zone, x.QType, x.RCode}] = x.Count
		sum += x.Count
	}
	if sum != 6 || totals[totalKey{"example.com.", "AAAA", "NOERROR"}] != 3 || totals[totalKey{"sub.example.com.", "A", "NXDOMAIN"}] != 1 ||
		totals[totalKey{wire.NoZone, "TXT", "REFUSED"}] != 1 || totals[totalKey{"example.com.", "A", ""}] != 1 {
		t.Fatalf("totals: %+v", d.Totals)
	}
	// With a cap of one name, the busiest name stays and the rest fold into Other rows that still add up.
	var names, other uint64
	top := ""
	for _, n := range d.Names {
		names += n.Count
		if n.QName == wire.Other {
			other += n.Count
		} else {
			top = n.QName
		}
	}
	if names != sum || other != 3 || top != "www.example.com." {
		t.Fatalf("names: %+v", d.Names)
	}
	if len(d.Clients) != 1 || d.Clients[0] != (wire.Client{Subnet: "192.0.2.0/24", Count: 6}) {
		t.Fatalf("clients: %+v", d.Clients)
	}
}

func TestSplitKeepsRowsDisjointAndComplete(t *testing.T) {
	d := wire.Dnstap{MinuteMs: 60000}
	for i := 0; i < 300; i++ {
		d.Totals = append(d.Totals, wire.Total{Zone: strings.Repeat("z", 50) + string(rune('a'+i%26)), QType: "A", RCode: "NOERROR", Count: 1})
		d.Names = append(d.Names, wire.Name{Zone: "z.", QName: strings.Repeat("n", 100) + string(rune('a'+i%26)), QType: "A", RCode: "NOERROR", Count: 2})
	}
	parts := split(d, 4096)
	if len(parts) < 2 {
		t.Fatalf("expected several parts, got %d", len(parts))
	}
	var totals, names int
	for i, p := range parts {
		if p.Part != i || p.Parts != len(parts) || p.MinuteMs != 60000 {
			t.Fatalf("part %d header: %+v", i, p)
		}
		totals += len(p.Totals)
		names += len(p.Names)
	}
	if totals != 300 || names != 300 {
		t.Fatalf("rows lost: %d totals, %d names", totals, names)
	}
	if one := split(wire.Dnstap{MinuteMs: 1, Totals: d.Totals[:2]}, 1<<20); len(one) != 1 || one[0].Parts != 0 {
		t.Fatalf("small bucket split: %+v", one)
	}
}

func TestAcksClearPendingAndTrimKeepsNewest(t *testing.T) {
	m := &Module{Now: time.Now}
	var pending []pendingPart
	for i := 0; i < wire.MaxPending+5; i++ {
		pending = append(pending, pendingPart{d: wire.Dnstap{MinuteMs: int64(i) * 60000}})
	}
	pending = trimPending(pending)
	if len(pending) != wire.MaxPending || pending[0].d.MinuteMs != 5*60000 {
		t.Fatalf("trim kept %d starting at %d", len(pending), pending[0].d.MinuteMs)
	}
	m.pending = pending
	r := &runner{m: m}
	ack := func(a wire.Ack) {
		env, _ := encodeEnv(wire.MsgAck, a)
		r.handle(env)
	}
	ack(wire.Ack{MinuteMs: 5 * 60000, Error: "storage failed"})
	if len(m.pending) != wire.MaxPending {
		t.Fatal("a failed ack removed the part")
	}
	ack(wire.Ack{MinuteMs: 5 * 60000})
	if len(m.pending) != wire.MaxPending-1 || m.pending[0].d.MinuteMs != 6*60000 {
		t.Fatal("an ack did not remove its part")
	}
}

func TestTapCheckLeavesALiveSocketAlone(t *testing.T) {
	path := filepath.Join(shortDir(t), "dnstap.sock")
	tp := &tap{path: path, agg: &aggregator{}, now: time.Now}
	if msg, err := tp.check(); err != nil || !strings.Contains(msg, "can be created") {
		t.Fatalf("missing socket: %q %v", msg, err)
	}
	ln, err := tp.open()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if msg, err := tp.check(); err != nil || !strings.Contains(msg, "in use") {
		t.Fatalf("live socket: %q %v", msg, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("check removed a live socket")
	}
	plain := filepath.Join(filepath.Dir(path), "plain")
	os.WriteFile(plain, nil, 0o600)
	if _, err := (&tap{path: plain}).check(); err == nil {
		t.Fatal("a regular file passed as a socket")
	}
}
