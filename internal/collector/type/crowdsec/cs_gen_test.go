package crowdsec

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"certainstats/internal/collector/type/crowdsec/wire"
)

const (
	rgMinuteMs = int64(60_000)
	rgDayMs    = 24 * hourMs
	rgUser     = "6f1d2c3b-4a5e-4f60-8b7a-9c0d1e2f3a4b"
)

// rgFirst is the first generated day.
var rgFirst = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

// rgGen makes deterministic Local API alerts, as close to real ones as the
// record study needs: an IP fixes its range, country and ASN; scenarios, IPs
// and countries are Zipf-distributed; UUIDs are random v4 (they do not
// compress); about 5% of messages are not the usual template; about 5% of
// alerts are about a range rather than an IP.
type rgGen struct {
	ips       []string
	ipRange   []string
	ipCountry []string
	ipASN     []int
	asName    map[string]string
	scenarios []string
	perDay    int
}

func newRGGen(perDay int) *rgGen {
	r := rand.New(rand.NewSource(42))
	g := &rgGen{perDay: perDay, asName: map[string]string{}}
	countries := make([]string, 120)
	for i := range countries {
		countries[i] = fmt.Sprintf("%c%c", 'A'+i/26%26, 'A'+i%26)
	}
	const nASN, nIP = 3000, 50000
	for i := range nASN {
		g.asName[strconv.Itoa(64500+i)] = fmt.Sprintf("Example Net %d", i)
	}
	cz := rand.NewZipf(r, 1.2, 4, uint64(len(countries)-1))
	az := rand.NewZipf(r, 1.1, 8, nASN-1)
	nRange := nIP / 4
	rangeCountry := make([]string, nRange)
	rangeASN := make([]int, nRange)
	rangeBase := make([][3]int, nRange)
	for i := range nRange {
		rangeCountry[i] = countries[cz.Uint64()]
		rangeASN[i] = 64500 + int(az.Uint64())
		rangeBase[i] = [3]int{1 + r.Intn(222), r.Intn(256), r.Intn(256)}
	}
	for range nIP {
		ri := r.Intn(nRange)
		b := rangeBase[ri]
		g.ips = append(g.ips, fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], 1+r.Intn(254)))
		g.ipRange = append(g.ipRange, fmt.Sprintf("%d.%d.%d.0/24", b[0], b[1], b[2]))
		g.ipCountry = append(g.ipCountry, rangeCountry[ri])
		g.ipASN = append(g.ipASN, rangeASN[ri])
	}
	for i := range 40 {
		g.scenarios = append(g.scenarios, fmt.Sprintf("crowdsecurity/scenario-%02d", i))
	}
	return g
}

func rgUUID(r *rand.Rand) string {
	return fmt.Sprintf("%08x-%04x-4%03x-%x%03x-%012x", r.Uint32(), r.Intn(1<<16), r.Intn(1<<12), 8+r.Intn(4), r.Intn(1<<12), r.Int63n(1<<48))
}

// rgMessage is the message CrowdSec writes for an alert; the record schema's
// template rebuilds exactly this.
func rgMessage(ip, scenario string, events int64, over time.Duration) string {
	return fmt.Sprintf("Ip %s performed '%s' (%d events over %s)", ip, scenario, events, over)
}

// day returns one collector's alerts for one day, per minute.
func (g *rgGen) day(collector int, day int64) [][]wire.Alert {
	r := rand.New(rand.NewSource(int64(collector)*1_000_003 + day))
	iz := rand.NewZipf(r, 1.05, 2, uint64(len(g.ips)-1))
	sz := rand.NewZipf(r, 1.3, 2, uint64(len(g.scenarios)-1))
	start := rgFirst + day*rgDayMs
	out := make([][]wire.Alert, 1440)
	mean := float64(g.perDay) / 1440
	seq := 0
	for m := range out {
		n := int(mean)
		if r.Float64() < mean-float64(n) {
			n++
		}
		for range n {
			seq++
			ip := int(iz.Uint64())
			at := time.UnixMilli(start + int64(m)*rgMinuteMs + int64(r.Intn(60))*1000).UTC()
			over := time.Duration(1+r.Intn(120)) * time.Second
			sc := g.scenarios[sz.Uint64()]
			events := int32(1 + r.Intn(50))
			src := &wire.Source{Scope: "Ip", Value: g.ips[ip], IP: g.ips[ip], Range: g.ipRange[ip],
				ASNumber: strconv.Itoa(g.ipASN[ip]), ASName: g.asName[strconv.Itoa(g.ipASN[ip])], CN: g.ipCountry[ip]}
			if r.Intn(20) == 0 {
				src.Scope, src.Value, src.IP = "Range", g.ipRange[ip], ""
			}
			a := wire.Alert{
				ID:          int64(seq),
				UUID:        rgUUID(r),
				MachineID:   fmt.Sprintf("machine-%d-%d", collector, r.Intn(3)),
				CreatedAt:   at.Add(over + time.Second).Format(time.RFC3339),
				StartAt:     at.Format(time.RFC3339),
				StopAt:      at.Add(over).Format(time.RFC3339),
				Scenario:    sc,
				EventsCount: events,
				Source:      src,
			}
			a.Message = rgMessage(src.IP, sc, int64(events), over)
			if r.Intn(20) == 0 {
				a.Message = fmt.Sprintf("%s: manual note %d", src.Value, r.Intn(10000))
			}
			nd := 0
			switch x := r.Float64(); {
			case x < 0.6:
				nd = 1
			case x < 0.7:
				nd = 2
			}
			for range nd {
				typ := "ban"
				if r.Float64() < 0.1 {
					typ = "captcha"
				}
				dur := time.Duration(1+r.Intn(24)) * time.Hour
				a.Decisions = append(a.Decisions, wire.Decision{UUID: rgUUID(r), Origin: "crowdsec", Type: typ, Scope: src.Scope,
					Value: src.Value, Duration: dur.String(), Until: at.Add(dur).Format(time.RFC3339), Scenario: sc})
			}
			out[m] = append(out[m], a)
		}
	}
	return out
}
