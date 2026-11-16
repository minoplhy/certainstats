package crowdsec

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// field is one filterable alert attribute. Rollup fields are also columns of the
// hourly rollup, so charts filtered by them can read it.
type field struct {
	Param, Column string
	Rollup        bool
}

var fields = []field{
	{"ip", "ip", false},
	{"range", "ip_range", false},
	{"scenario", "scenario", true},
	{"country", "country", true},
	{"machine", "machine_id", true},
	{"asn", "asn", false},
	{"action", "action", true},
	{"collector", "collector_id", false},
}

func fieldByParam(p string) (field, bool) {
	for _, f := range fields {
		if f.Param == p {
			return f, true
		}
	}
	return field{}, false
}

const (
	maxValuesPerField = 20
	maxValueLen       = 200
	maxSpanMs         = int64(20 * 365 * 24 * 3_600_000)
	rollupAfterMs     = int64(7 * 24 * 3_600_000)
)

// Filter is the shared filter model: a bounded time range plus include and exclude
// values per field. Every read of alert data takes one, so no query is unbounded.
type Filter struct {
	Start, End int64
	Include    map[string][]string
	Exclude    map[string][]string
	Q          string
	TZOffset   int64 // minutes east of UTC, for day buckets
}

var errRange = errors.New("invalid time range")

// ParseFilter reads start/end (Unix ms) or hours, then field=value and not_field=value,
// repeated for several values. Comma lists are never split.
func ParseFilter(q url.Values, now time.Time) (Filter, error) {
	f := Filter{Include: map[string][]string{}, Exclude: map[string][]string{}}
	nowMs := now.UnixMilli()
	switch {
	case q.Get("start") != "" || q.Get("end") != "":
		start, e1 := strconv.ParseInt(q.Get("start"), 10, 64)
		end, e2 := strconv.ParseInt(q.Get("end"), 10, 64)
		if e1 != nil || e2 != nil || start < 0 || end <= start || end > nowMs+10*60_000 || end-start > maxSpanMs {
			return f, errRange
		}
		f.Start, f.End = start, end
	default:
		hours := int64(24)
		if raw := q.Get("hours"); raw != "" {
			h, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || h <= 0 || h*3_600_000 > maxSpanMs {
				return f, errRange
			}
			hours = h
		}
		f.Start, f.End = nowMs-hours*3_600_000, nowMs
	}
	for _, fd := range fields {
		for _, side := range []struct {
			param string
			dst   map[string][]string
		}{{fd.Param, f.Include}, {"not_" + fd.Param, f.Exclude}} {
			values := q[side.param]
			if len(values) > maxValuesPerField {
				return f, errors.New("too many values for " + side.param)
			}
			for _, v := range values {
				if len(v) > maxValueLen {
					return f, errors.New("value too long for " + side.param)
				}
				side.dst[fd.Param] = append(side.dst[fd.Param], v)
			}
		}
	}
	f.Q = strings.TrimSpace(q.Get("q"))
	if len(f.Q) > maxValueLen {
		return f, errors.New("search text too long")
	}
	if raw := q.Get("tz_offset"); raw != "" {
		off, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || off < -14*60 || off > 14*60 {
			return f, errors.New("invalid tz_offset")
		}
		f.TZOffset = off
	}
	return f, nil
}

// NeedsRaw reports whether the filter uses anything the rollup does not keep.
func (f Filter) NeedsRaw() bool {
	if f.Q != "" {
		return true
	}
	for _, fd := range fields {
		if !fd.Rollup && (len(f.Include[fd.Param]) > 0 || len(f.Exclude[fd.Param]) > 0) {
			return true
		}
	}
	return false
}

func likeEscape(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// where compiles the filter to a parameterized WHERE clause. Column names come only
// from the fixed field table; every value is a placeholder.
func (f Filter) where(userID string, rollup, withTime bool, qual string) (string, []any) {
	clauses := []string{qual + "user_id = ?"}
	args := []any{userID}
	if withTime {
		if rollup {
			clauses = append(clauses, "hour >= ?", "hour < ?")
			args = append(args, hourOf(f.Start), f.End)
		} else {
			clauses = append(clauses, qual+"started_at >= ?", qual+"started_at < ?")
			args = append(args, f.Start, f.End)
		}
	}
	for _, fd := range fields {
		if rollup && !fd.Rollup {
			continue
		}
		if vals := f.Include[fd.Param]; len(vals) > 0 {
			clauses = append(clauses, qual+fd.Column+" IN ("+placeholders(len(vals))+")")
			for _, v := range vals {
				args = append(args, v)
			}
		}
		if vals := f.Exclude[fd.Param]; len(vals) > 0 {
			clauses = append(clauses, qual+fd.Column+" NOT IN ("+placeholders(len(vals))+")")
			for _, v := range vals {
				args = append(args, v)
			}
		}
	}
	if f.Q != "" && !rollup {
		pattern := likeEscape(f.Q)
		cols := []string{"ip", "scenario", "machine_id", "country", "as_name", "asn", "value", "ip_range"}
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = qual + c + ` LIKE ? ESCAPE '\'`
			args = append(args, pattern)
		}
		clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
	}
	return strings.Join(clauses, " AND "), args
}
