//go:build rblkreport

// The 30-day records study: crowdsec.db against Stores whose alerts live in
// rblk (the slim and block engines), a day at a time, logging size, write
// cost, peak memory and every screen's time. The answers are checked by
// TestRblkMatchesSQL, which runs without this tag.
//
//	RECREPORT_DIR=... go test -tags rblkreport -run TestRecordsReport -timeout 0 -v ./internal/collector/type/crowdsec/
package crowdsec

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"certainstats/internal/rblk"
)

// openRecStore is a Store whose alerts live in one rblk engine under dir,
// with its rollup in its own crowdsec.db beside it.
func openRecStore(dir string, e rblk.Engine) (*Store, error) {
	st, err := Open(dir + "-crowdsec.db")
	if err != nil {
		return nil, err
	}
	rs, err := rblk.Open(dir, rbSchema(), e)
	if err != nil {
		st.Close()
		return nil, err
	}
	st.rec = &recStore{rs}
	return st, nil
}

type rgCheckpoint struct {
	Day      int64               `json:"day"`
	PerDay   int                 `json:"alerts_per_day"`
	Bytes    map[string]int64    `json:"bytes"`
	Detail   map[string]any      `json:"detail"`
	WriteS   map[string]float64  `json:"write_s"`
	PeakKB   map[string]int64    `json:"peak_rss_kb"`
	PeakHeap map[string]uint64   `json:"peak_heap"`
	QueryMs  map[string][3]int64 `json:"query_ms"` // screen → sql, slim, blocks
	At       string              `json:"at"`
}

func rgPeak(fn func() error) (rssKB int64, heap uint64, err error) {
	runtime.GC()
	_ = os.WriteFile("/proc/self/clear_refs", []byte("5"), 0)
	var mu sync.Mutex
	stop, done := make(chan struct{}), make(chan struct{})
	read := func() uint64 {
		s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
		metrics.Read(s)
		return s[0].Value.Uint64()
	}
	go func() {
		defer close(done)
		tk := time.NewTicker(100 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				h := read()
				mu.Lock()
				heap = max(heap, h)
				mu.Unlock()
			}
		}
	}()
	err = fn()
	close(stop)
	<-done
	heap = max(heap, read())
	return rgStatusKB("VmHWM"), heap, err
}

func rgStatusKB(field string) int64 {
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, field+":"); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")), 10, 64)
			return n
		}
	}
	return 0
}

// tableBytes sizes a SQLite file's tables with their indexes.
func tableBytes(db *sql.DB) map[string]int64 {
	out := map[string]int64{}
	rows, err := db.Query(`SELECT COALESCE(m.tbl_name, d.name), SUM(d.pgsize) FROM dbstat d LEFT JOIN sqlite_master m ON m.name = d.name GROUP BY 1`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var n int64
		if rows.Scan(&name, &n) == nil {
			out[name] = n
		}
	}
	return out
}

func rgEnvInt(t *testing.T, key string, def int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		t.Fatalf("%s must be a positive number", key)
	}
	return n
}

// TestRecordsReport grows crowdsec.db and both record engines a day at a
// time and logs size, write cost, peak memory and every screen's time.
func TestRecordsReport(t *testing.T) {
	dir := os.Getenv("RECREPORT_DIR")
	if dir == "" {
		t.Skip("set RECREPORT_DIR to run the records report")
	}
	ctx := context.Background()
	perDay := rgEnvInt(t, "RECREPORT_ALERTS_PER_DAY", 20000)
	days := rgEnvInt(t, "RECREPORT_DAYS", 30)
	run := filepath.Join(dir, fmt.Sprintf("rec-a%d", perDay))
	os.RemoveAll(run)
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := Open(filepath.Join(run, "crowdsec.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	names := []string{"slim", "blocks"}
	var ks []*Store
	for _, e := range []rblk.Engine{rblk.Slim, rblk.Blocks} {
		s, err := openRecStore(filepath.Join(run, string(e)), e)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		ks = append(ks, s)
	}
	all := []*Store{st, ks[0], ks[1]}
	slimDB, err := sql.Open("sqlite", filepath.Join(run, "slim", "records.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer slimDB.Close()
	g := newRGGen(perDay)
	out, err := os.Create(filepath.Join(dir, fmt.Sprintf("rec-a%d.jsonl", perDay)))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	for d := range int64(days) {
		day := rgDay(g, 1, d)
		start := rgFirst + d*rgDayMs
		cp := rgCheckpoint{Day: d + 1, PerDay: perDay, Bytes: map[string]int64{}, Detail: map[string]any{}, WriteS: map[string]float64{},
			PeakKB: map[string]int64{}, PeakHeap: map[string]uint64{}, QueryMs: map[string][3]int64{}}
		var e error
		for i, s := range all {
			name := append([]string{"sql"}, names...)[i]
			cp.PeakKB[name], cp.PeakHeap[name], e = rgPeak(func() error {
				secs, err := rgFeed(ctx, day, start, []*Store{s}, false)
				cp.WriteS[name] = secs[0]
				return err
			})
			if e != nil {
				t.Fatal(e)
			}
		}
		end := start + rgDayMs
		sqlT := tableBytes(st.read)
		cp.Detail["sql_tables"] = sqlT
		cp.Bytes["sql_records"] = sqlT["alerts"] + sqlT["decisions"]
		slimT := tableBytes(slimDB)
		cp.Detail["slim_tables"] = slimT
		cp.Bytes["slim"] = slimT["rec"] + slimT["child"] + slimT["strs"] + slimT["meta"]
		bs, err := ks[1].rec.s.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cp.Bytes["blocks"] = bs.FileBytes
		cp.Detail["blocks"] = bs
		// Every screen at the current size.
		ipTop, ipRare := g.ips[0], g.ips[40000]
		var maxID int64
		st.read.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM alerts`).Scan(&maxID)
		type screen struct {
			name string
			run  func(s *Store) error
		}
		var screens []screen
		for _, w := range []struct {
			name string
			ms   int64
		}{{"24h", rgDayMs}, {"7d", 7 * rgDayMs}, {"30d", 30 * rgDayMs}} {
			f := rgFilter(max(end-w.ms, rgFirst), end, nil, nil, "")
			fs := rgFilter(max(end-w.ms, rgFirst), end, map[string][]string{"scenario": {g.scenarios[0]}}, nil, "")
			fq := rgFilter(max(end-w.ms, rgFirst), end, nil, nil, ipTop[:strings.LastIndex(ipTop, ".")])
			screens = append(screens,
				screen{"list time " + w.name, func(s *Store) error { _, err := s.Alerts(ctx, rgUser, f, "time", true, 1, 50); return err }},
				screen{"list time page 40 " + w.name, func(s *Store) error { _, err := s.Alerts(ctx, rgUser, f, "time", true, 40, 50); return err }},
				screen{"list by ip " + w.name, func(s *Store) error { _, err := s.Alerts(ctx, rgUser, f, "ip", false, 1, 50); return err }},
				screen{"list scenario " + w.name, func(s *Store) error { _, err := s.Alerts(ctx, rgUser, fs, "time", true, 1, 50); return err }},
				screen{"search " + w.name, func(s *Store) error { _, err := s.Alerts(ctx, rgUser, fq, "time", true, 1, 50); return err }},
			)
			// With rblk every range reads records, so the count screens are
			// timed on 30 days too; crowdsec.db answers those from its rollup.
			{
				ipf, _ := fieldByParam("ip")
				scf, _ := fieldByParam("scenario")
				screens = append(screens,
					screen{"summary " + w.name, func(s *Store) error { _, err := s.Summary(ctx, rgUser, f, end); return err }},
					screen{"timeline " + w.name, func(s *Store) error { _, err := s.Timeline(ctx, rgUser, f); return err }},
					screen{"groups ip " + w.name, func(s *Store) error { _, err := s.Groups(ctx, rgUser, f, ipf, 10); return err }},
					screen{"groups scenario " + w.name, func(s *Store) error { _, err := s.Groups(ctx, rgUser, f, scf, 10); return err }},
				)
			}
		}
		screens = append(screens,
			screen{"detail", func(s *Store) error { _, err := s.Alert(ctx, rgUser, maxID/3+1); return err }},
			screen{"ip page top", func(s *Store) error { _, err := s.IP(ctx, rgUser, ipTop); return err }},
			screen{"ip page rare", func(s *Store) error { _, err := s.IP(ctx, rgUser, ipRare); return err }},
		)
		// Each screen runs three times per store and keeps the median; the
		// order of the stores rotates so none always runs first.
		for si, sc := range screens {
			runs := [3][]int64{}
			for rep := range 3 {
				for o := range 3 {
					which := (si + rep + o) % 3
					t0 := time.Now()
					if err := sc.run(all[which]); err != nil {
						t.Fatal(sc.name, err)
					}
					runs[which] = append(runs[which], time.Since(t0).Microseconds())
				}
			}
			var ms [3]int64
			for i := range runs {
				slices.Sort(runs[i])
				ms[i] = (runs[i][1] + 500) / 1000
			}
			cp.QueryMs[sc.name] = ms
		}
		cp.At = time.Now().Format(time.RFC3339)
		line, _ := json.Marshal(cp)
		out.Write(append(line, '\n'))
		slowest := [3]int64{}
		slowName := [3]string{}
		for name, ms := range cp.QueryMs {
			for i := range ms {
				if ms[i] > slowest[i] {
					slowest[i], slowName[i] = ms[i], name
				}
			}
		}
		t.Logf("day %d: records sql %.1f MB, slim %.1f MB, blocks %.1f MB | write %.1f / %.1f / %.1f s | peak %d / %d / %d MB | slowest sql %d ms (%s), slim %d ms (%s), blocks %d ms (%s)",
			d+1, float64(cp.Bytes["sql_records"])/1e6, float64(cp.Bytes["slim"])/1e6, float64(cp.Bytes["blocks"])/1e6,
			cp.WriteS["sql"], cp.WriteS["slim"], cp.WriteS["blocks"], cp.PeakKB["sql"]/1024, cp.PeakKB["slim"]/1024, cp.PeakKB["blocks"]/1024,
			slowest[0], slowName[0], slowest[1], slowName[1], slowest[2], slowName[2])
	}
}
