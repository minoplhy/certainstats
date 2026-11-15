package crowdsec

import (
	"certainstats/internal/crowdsec/wire"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"
)

type decisionRow struct {
	UUID, Type, Origin, Scope, Value, Duration string
	UntilMs                                    int64
}

type alertRow struct {
	UUID, Machine, Scenario, Message, Kind string
	IP, Range, Scope, Value                string
	Country, ASN, ASName, Action           string
	LapiID, Events                         int64
	Simulated                              bool
	StartedAt, StoppedAt, CreatedAt        int64
	Decisions                              []decisionRow
}

// parseTime reads a Local API timestamp (RFC 3339 or Go's String() form); zero when missing or invalid.
func parseTime(v string) int64 {
	t, ok := wire.ParseTime(v)
	if !ok {
		return 0
	}
	return t.UnixMilli()
}

func clip(v string, n int) string {
	v = strings.TrimSpace(v)
	if len(v) > n {
		return v[:n]
	}
	return v
}

// normalize turns a Local API alert into a row. The alert's own start time is its
// time; receive time is only a last resort, so late deliveries land in the right hour.
func normalize(collectorID string, a *wire.Alert, receivedAt int64) alertRow {
	row := alertRow{
		UUID:      clip(a.UUID, 64),
		Machine:   clip(a.MachineID, 200),
		Scenario:  clip(a.Scenario, 200),
		Message:   clip(a.Message, 500),
		Kind:      clip(a.Kind, 50),
		LapiID:    a.ID,
		Events:    int64(max(a.EventsCount, 0)),
		Simulated: a.Simulated,
		CreatedAt: parseTime(a.CreatedAt),
		StoppedAt: parseTime(a.StopAt),
	}
	row.StartedAt = parseTime(a.StartAt)
	if row.StartedAt == 0 {
		row.StartedAt = row.CreatedAt
	}
	if row.StartedAt == 0 {
		row.StartedAt = receivedAt
	}
	if row.UUID == "" {
		sum := sha256.Sum256([]byte(collectorID + "\x00" + strconv.FormatInt(a.ID, 10) + "\x00" + a.CreatedAt + "\x00" + a.Scenario))
		row.UUID = "h:" + hex.EncodeToString(sum[:16])
	}
	if src := a.Source; src != nil {
		row.Scope = clip(src.Scope, 50)
		row.Value = clip(src.Value, 200)
		row.IP = clip(src.IP, 64)
		if row.IP == "" && strings.EqualFold(src.Scope, "ip") {
			row.IP = row.Value
		}
		row.Range = clip(src.Range, 64)
		row.Country = strings.ToUpper(clip(src.CN, 8))
		row.ASN = clip(src.ASNumber, 20)
		row.ASName = clip(src.ASName, 200)
	}
	types := map[string]bool{}
	for _, d := range a.Decisions {
		dr := decisionRow{UUID: clip(d.UUID, 64), Type: clip(d.Type, 50), Origin: clip(d.Origin, 50), Scope: clip(d.Scope, 50), Value: clip(d.Value, 200), Duration: clip(d.Duration, 50)}
		dr.UntilMs = parseTime(d.Until)
		// The Local API reports the time left when it was read; the collector fills Until
		// from it, so receive time is only a close fallback for older collectors.
		if dr.UntilMs == 0 {
			if dur, err := time.ParseDuration(d.Duration); err == nil {
				dr.UntilMs = receivedAt + dur.Milliseconds()
			}
		}
		if dr.Type != "" {
			types[dr.Type] = true
		}
		row.Decisions = append(row.Decisions, dr)
	}
	row.Action = "none"
	if len(types) > 0 {
		list := make([]string, 0, len(types))
		for t := range types {
			list = append(list, t)
		}
		sort.Strings(list)
		row.Action = strings.Join(list, "+")
	}
	return row
}
