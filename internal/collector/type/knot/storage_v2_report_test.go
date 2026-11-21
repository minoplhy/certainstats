//go:build knotreport

// Prototype of a fixed dnstap schema, measured by the storage report with
// KNOTREPORT_STORE=v2. Nothing here ships; it answers whether SQLite holds up
// once the schema is fixed:
//   - every key column is an integer: strings live once in dnstap2_keys and
//     client network keys in dnstap2_nets (kept apart so a CIDR is a key range),
//   - the primary key is (uid, bucket, cid, keys…), so any time window is a
//     range of the key, with one secondary index (names by qname),
//   - the rollup folds client rows below the cap per network only.
//
// TestV2MatchesStore checks it answers the page's questions exactly as Store does.
package knot

import (
	"context"
	"database/sql"
	"fmt"
	"net/netip"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"certainstats/internal/collector/type/knot/wire"
)

type v2Store struct {
	write, read *sql.DB
	TopHourly   int

	mu      sync.RWMutex
	keys    map[string]int64 // dnstap2_keys
	strs    map[int64]string
	nets    map[string]int64 // dnstap2_nets, by stored network key
	netStrs map[int64]string
}

func v2Schema() []string {
	out := []string{
		`CREATE TABLE IF NOT EXISTS dnstap2_keys (id INTEGER PRIMARY KEY, s TEXT NOT NULL UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS dnstap2_nets (id INTEGER PRIMARY KEY, s TEXT NOT NULL UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS dnstap2_meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS dnstap2_owners (uid INTEGER PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS dnstap2_minutes (uid INTEGER NOT NULL, bucket INTEGER NOT NULL, cid INTEGER NOT NULL, queries_only INTEGER NOT NULL, PRIMARY KEY (uid, bucket, cid)) WITHOUT ROWID`,
	}
	for _, d := range dims {
		for _, grain := range []string{"minute", "hourly"} {
			table := "dnstap2_" + d.name + "_" + grain
			cols := ""
			for _, k := range d.keys {
				cols += k + " INTEGER NOT NULL, "
			}
			out = append(out, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (uid INTEGER NOT NULL, bucket INTEGER NOT NULL, cid INTEGER NOT NULL, %scount INTEGER NOT NULL, PRIMARY KEY (%s)) WITHOUT ROWID`,
				table, cols, v2PK(d.keys)))
			if d.name == "names" {
				out = append(out, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_qname ON %s(uid, qname, bucket)`, table, table))
			}
		}
	}
	return out
}

func v2PK(keys []string) string {
	return "uid, bucket, cid, " + strings.Join(keys, ", ")
}

func dimKeys(dim string) []string {
	for _, d := range dims {
		if d.name == dim {
			return d.keys
		}
	}
	return nil
}

func openV2(t *testing.T, path string) *v2Store {
	t.Helper()
	w, r := tracedDBs(t, path)
	for _, stmt := range v2Schema() {
		if _, err := w.Exec(stmt); err != nil {
			t.Fatalf("v2 schema: %v", err)
		}
	}
	s := &v2Store{write: w, read: r, TopHourly: 1000,
		keys: map[string]int64{}, strs: map[int64]string{}, nets: map[string]int64{}, netStrs: map[int64]string{}}
	for _, l := range []struct {
		table string
		fwd   map[string]int64
		back  map[int64]string
	}{{"dnstap2_keys", s.keys, s.strs}, {"dnstap2_nets", s.nets, s.netStrs}} {
		rows, err := r.Query(`SELECT id, s FROM ` + l.table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id int64
			var v string
			if err := rows.Scan(&id, &v); err != nil {
				t.Fatal(err)
			}
			l.fwd[v], l.back[id] = id, v
		}
		rows.Close()
	}
	// The fold keys exist from the start, so reads and the rollup never miss them.
	x, err := s.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{wire.Other, ""} {
		if _, err := x.key(context.Background(), v); err != nil {
			t.Fatal(err)
		}
	}
	if err := x.commit(); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *v2Store) Close() error {
	s.read.Close()
	return s.write.Close()
}

func (s *v2Store) reportTop() int { return s.TopHourly }

func (s *v2Store) keyID(v string) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.keys[v]
	return id, ok
}

func (s *v2Store) label(id int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.strs[id]
}

func (s *v2Store) netKeyStr(id int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.netStrs[id]
}

// v2Tx is a write transaction that interns strings; new IDs reach the cache
// only when it commits.
type v2Tx struct {
	s                *v2Store
	tx               *sql.Tx
	keyStmt, netStmt *sql.Stmt
	newKeys, newNets map[string]int64
}

func (s *v2Store) begin(ctx context.Context) (*v2Tx, error) {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &v2Tx{s: s, tx: tx, newKeys: map[string]int64{}, newNets: map[string]int64{}}, nil
}

func (x *v2Tx) intern(ctx context.Context, v, table string, stmt **sql.Stmt, cache map[string]int64, fresh map[string]int64) (int64, error) {
	x.s.mu.RLock()
	id, ok := cache[v]
	x.s.mu.RUnlock()
	if ok {
		return id, nil
	}
	if id, ok := fresh[v]; ok {
		return id, nil
	}
	if *stmt == nil {
		st, err := x.tx.PrepareContext(ctx, `INSERT INTO `+table+` (s) VALUES (?) ON CONFLICT (s) DO UPDATE SET s = excluded.s RETURNING id`)
		if err != nil {
			return 0, err
		}
		*stmt = st
	}
	if err := (*stmt).QueryRowContext(ctx, v).Scan(&id); err != nil {
		return 0, err
	}
	fresh[v] = id
	return id, nil
}

func (x *v2Tx) key(ctx context.Context, v string) (int64, error) {
	return x.intern(ctx, v, "dnstap2_keys", &x.keyStmt, x.s.keys, x.newKeys)
}

func (x *v2Tx) net(ctx context.Context, v string) (int64, error) {
	return x.intern(ctx, v, "dnstap2_nets", &x.netStmt, x.s.nets, x.newNets)
}

func (x *v2Tx) commit() error {
	if err := x.tx.Commit(); err != nil {
		return err
	}
	x.s.mu.Lock()
	for v, id := range x.newKeys {
		x.s.keys[v], x.s.strs[id] = id, v
	}
	for v, id := range x.newNets {
		x.s.nets[v], x.s.netStrs[id] = id, v
	}
	x.s.mu.Unlock()
	return nil
}

func (x *v2Tx) rollback() { x.tx.Rollback() }

// rows turns a dnstap part into integer key rows per dimension, count last.
func (x *v2Tx) rows(ctx context.Context, d wire.Dnstap) (map[string][][]int64, error) {
	out := map[string][][]int64{}
	keys := func(vs ...string) ([]int64, error) {
		ids := make([]int64, 0, len(vs)+1)
		for _, v := range vs {
			id, err := x.key(ctx, v)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, nil
	}
	for _, r := range d.Totals {
		k, err := keys(r.Zone, r.QType, r.RCode)
		if err != nil {
			return nil, err
		}
		out["totals"] = append(out["totals"], append(k, int64(r.Count)))
	}
	for _, r := range d.Names {
		k, err := keys(r.Zone, r.QName, r.QType, r.RCode)
		if err != nil {
			return nil, err
		}
		out["names"] = append(out["names"], append(k, int64(r.Count)))
	}
	// As Store.SaveDnstap: client rows without a name or with a bad subnet are skipped.
	for _, r := range d.Clients {
		nk, ok := netKeyOf(r.Subnet)
		if !ok || r.QName == "" {
			continue
		}
		net, err := x.net(ctx, nk)
		if err != nil {
			return nil, err
		}
		k, err := keys(r.Zone, r.QName, r.QType, r.RCode)
		if err != nil {
			return nil, err
		}
		out["clients"] = append(out["clients"], append(append([]int64{net}, k...), int64(r.Count)))
	}
	for _, r := range d.Sizes {
		out["sizes"] = append(out["sizes"], []int64{int64(r.UpTo), int64(r.Count)})
	}
	return out, nil
}

// put upserts rows of one table by key, as Store.SaveDnstap does.
func (x *v2Tx) put(ctx context.Context, table string, keys []string, uid, bucket, cid int64, rows [][]int64) error {
	if len(rows) == 0 {
		return nil
	}
	marks := strings.Repeat(", ?", len(keys))
	stmt, err := x.tx.PrepareContext(ctx, fmt.Sprintf(`INSERT INTO %s (uid, bucket, cid, %s, count) VALUES (?, ?, ?%s, ?)
		ON CONFLICT (%s) DO UPDATE SET count = excluded.count`, table, strings.Join(keys, ", "), marks, v2PK(keys)))
	if err != nil {
		return err
	}
	defer stmt.Close()
	args := make([]any, 0, len(keys)+4)
	for _, r := range rows {
		args = append(args[:0], uid, bucket, cid)
		for _, v := range r {
			args = append(args, v)
		}
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return err
		}
	}
	return nil
}

func (x *v2Tx) owner(ctx context.Context, userID, collectorID string) (uid, cid int64, err error) {
	if uid, err = x.key(ctx, userID); err != nil {
		return
	}
	if cid, err = x.key(ctx, collectorID); err != nil {
		return
	}
	_, err = x.tx.ExecContext(ctx, `INSERT INTO dnstap2_owners (uid) VALUES (?) ON CONFLICT DO NOTHING`, uid)
	return
}

func (s *v2Store) SaveDnstap(ctx context.Context, userID, collectorID string, d wire.Dnstap, now int64) error {
	bucket := d.MinuteMs - d.MinuteMs%minuteMs
	x, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer x.rollback()
	uid, cid, err := x.owner(ctx, userID, collectorID)
	if err != nil {
		return err
	}
	if _, err := x.tx.ExecContext(ctx, `INSERT INTO dnstap2_minutes (uid, bucket, cid, queries_only) VALUES (?, ?, ?, ?)
		ON CONFLICT (uid, bucket, cid) DO UPDATE SET queries_only = excluded.queries_only`, uid, bucket, cid, b2i(d.QueriesOnly)); err != nil {
		return err
	}
	rows, err := x.rows(ctx, d)
	if err != nil {
		return err
	}
	for _, dim := range dims {
		if err := x.put(ctx, "dnstap2_"+dim.name+"_minute", dim.keys, uid, bucket, cid, rows[dim.name]); err != nil {
			return err
		}
	}
	return x.commit()
}

func (s *v2Store) reportHourly(ctx context.Context, user, col string, d wire.Dnstap) error {
	x, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer x.rollback()
	uid, cid, err := x.owner(ctx, user, col)
	if err != nil {
		return err
	}
	rows, err := x.rows(ctx, d)
	if err != nil {
		return err
	}
	for _, dim := range dims {
		if err := x.put(ctx, "dnstap2_"+dim.name+"_hourly", dim.keys, uid, d.MinuteMs, cid, rows[dim.name]); err != nil {
			return err
		}
	}
	return x.commit()
}

func (s *v2Store) metaGet(ctx context.Context, key string) (int64, error) {
	var v int64
	err := s.read.QueryRowContext(ctx, `SELECT value FROM dnstap2_meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

func (s *v2Store) rolledUntil(ctx context.Context) (int64, error) {
	return s.metaGet(ctx, "rolled_until")
}

func (s *v2Store) owners(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT uid FROM dnstap2_owners ORDER BY uid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Rollup mirrors Store.Rollup: settled hours only, at most rollBatch per call.
func (s *v2Store) Rollup(ctx context.Context, now int64) (int, error) {
	until, err := s.rolledUntil(ctx)
	if err != nil {
		return 0, err
	}
	if until == 0 {
		var oldest *int64
		if err := s.read.QueryRowContext(ctx, `SELECT MIN(bucket) FROM dnstap2_minutes`).Scan(&oldest); err != nil {
			return 0, err
		}
		if oldest == nil {
			return 0, nil
		}
		until = hourOf(*oldest)
	}
	target := hourOf(now - rollLag.Milliseconds())
	done := 0
	for h := until; h < target && done < rollBatch && ctx.Err() == nil; h += hourMs {
		if err := s.rollHour(ctx, h); err != nil {
			return done, err
		}
		done++
	}
	return done, ctx.Err()
}

func (s *v2Store) rollHour(ctx context.Context, h int64) error {
	x, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer x.rollback()
	// Every read and delete names the owner, so it is a range of the primary key.
	users, err := s.owners(ctx, x.tx)
	if err != nil {
		return err
	}
	for _, uid := range users {
		for _, d := range dims {
			hourT, minuteT := "dnstap2_"+d.name+"_hourly", "dnstap2_"+d.name+"_minute"
			if _, err := x.tx.ExecContext(ctx, `DELETE FROM `+hourT+` WHERE uid = ? AND bucket = ?`, uid, h); err != nil {
				return err
			}
			if d.name == "names" || d.name == "clients" {
				continue
			}
			keys := strings.Join(d.keys, ", ")
			if _, err := x.tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (uid, bucket, cid, %s, count)
				SELECT uid, ?, cid, %s, SUM(count) FROM %s WHERE uid = ? AND bucket >= ? AND bucket < ? GROUP BY cid, %s`,
				hourT, keys, keys, minuteT, keys), h, uid, h, h+hourMs); err != nil {
				return err
			}
		}
		if err := s.rollTop(ctx, x, uid, h, "names"); err != nil {
			return err
		}
		if err := s.rollTop(ctx, x, uid, h, "clients"); err != nil {
			return err
		}
	}
	if _, err := x.tx.ExecContext(ctx, `INSERT INTO dnstap2_meta (key, value) VALUES ('rolled_until', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, h+hourMs); err != nil {
		return err
	}
	return x.commit()
}

// rollTop keeps the top TopHourly rows per collector and hour, as Store.rollTop
// does. The rest fold into Other rows: names per zone, qtype and rcode; client
// rows per network only.
func (s *v2Store) rollTop(ctx context.Context, x *v2Tx, uid, h int64, dim string) error {
	keys := dimKeys(dim)
	qi := len(keys) - 3 // qname
	cols := strings.Join(keys, ", ")
	rows, err := x.tx.QueryContext(ctx, fmt.Sprintf(`SELECT cid, %s, SUM(count) FROM dnstap2_%s_minute
		WHERE uid = ? AND bucket >= ? AND bucket < ? GROUP BY cid, %s`, cols, dim, cols), uid, h, h+hourMs)
	if err != nil {
		return err
	}
	type row struct {
		key  [5]int64
		sort string
		n    int64
	}
	groups := map[int64][]row{}
	for rows.Next() {
		var cid int64
		var r row
		dest := []any{&cid}
		for i := range keys {
			dest = append(dest, &r.key[i])
		}
		if err := rows.Scan(append(dest, &r.n)...); err != nil {
			rows.Close()
			return err
		}
		groups[cid] = append(groups[cid], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	other, _ := s.keyID(wire.Other)
	fold := func(k [5]int64) [5]int64 {
		if dim == "clients" {
			k[1], k[2], k[3], k[4] = other, other, other, other
		} else {
			k[qi] = other
		}
		return k
	}
	// Ties sort by the stored strings, like Store.rollTop, so both keep the same rows.
	sortKey := func(k [5]int64) string {
		parts := make([]string, len(keys))
		for i := range keys {
			if keys[i] == "net" {
				parts[i] = s.netKeyStr(k[i])
			} else {
				parts[i] = s.label(k[i])
			}
		}
		return strings.Join(parts, "\x00")
	}
	marks := strings.Repeat(", ?", len(keys))
	stmt, err := x.tx.PrepareContext(ctx, fmt.Sprintf(`INSERT INTO dnstap2_%s_hourly (uid, bucket, cid, %s, count) VALUES (?, ?, ?%s, ?)`, dim, cols, marks))
	if err != nil {
		return err
	}
	defer stmt.Close()
	put := func(cid int64, k [5]int64, n int64) error {
		args := []any{uid, h, cid}
		for i := range keys {
			args = append(args, k[i])
		}
		_, err := stmt.ExecContext(ctx, append(args, n)...)
		return err
	}
	cids := make([]int64, 0, len(groups))
	for cid := range groups {
		cids = append(cids, cid)
	}
	sort.Slice(cids, func(i, j int) bool { return cids[i] < cids[j] })
	for _, cid := range cids {
		list := groups[cid]
		named := list[:0:0]
		folded := map[[5]int64]int64{}
		for _, r := range list {
			if r.key[qi] == other {
				folded[fold(r.key)] += r.n
				continue
			}
			r.sort = sortKey(r.key)
			named = append(named, r)
		}
		sort.Slice(named, func(i, j int) bool {
			if named[i].n != named[j].n {
				return named[i].n > named[j].n
			}
			return named[i].sort < named[j].sort
		})
		if len(named) > s.TopHourly {
			for _, r := range named[s.TopHourly:] {
				folded[fold(r.key)] += r.n
			}
			named = named[:s.TopHourly]
		}
		for _, r := range named {
			if err := put(cid, r.key, r.n); err != nil {
				return err
			}
		}
		for k, n := range folded {
			if err := put(cid, k, n); err != nil {
				return err
			}
		}
	}
	return nil
}

// deleteBatches repeats a bounded DELETE, as Store.deleteBatches does.
func (s *v2Store) deleteBatches(ctx context.Context, query string, args ...any) (int64, error) {
	var total int64
	for {
		res, err := s.write.ExecContext(ctx, query, append(args, pruneBatch)...)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < pruneBatch || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

// Prune applies retention per owner, so each delete is a range of the
// primary key, and drops the rows of owners no longer known.
func (s *v2Store) Prune(ctx context.Context, keep Retention, now int64, users []string) (PruneResult, error) {
	var out PruneResult
	known := map[int64]bool{}
	for _, u := range users {
		if id, ok := s.keyID(u); ok {
			known[id] = true
		}
	}
	owners, err := s.owners(ctx, s.read)
	if err != nil {
		return out, err
	}
	del := func(table, pk string, cut int64, uid int64) (int64, error) {
		return s.deleteBatches(ctx, fmt.Sprintf(`DELETE FROM %s WHERE (%s) IN (SELECT %s FROM %s WHERE uid = ? AND bucket < ? LIMIT ?)`, table, pk, pk, table), uid, cut)
	}
	const all = int64(1) << 62 // a cut past every bucket
	for _, uid := range owners {
		minuteCut, hourCut := int64(0), int64(0)
		if !known[uid] {
			minuteCut, hourCut = all, all
		} else {
			if keep.MinuteMs > 0 {
				minuteCut = now - keep.MinuteMs
			}
			if keep.HourlyMs > 0 {
				hourCut = now - keep.HourlyMs
			}
		}
		for _, d := range dims {
			pk := v2PK(d.keys)
			if minuteCut > 0 {
				n, err := del("dnstap2_"+d.name+"_minute", pk, minuteCut, uid)
				out.Minute += n
				if err != nil {
					return out, err
				}
			}
			if hourCut > 0 {
				n, err := del("dnstap2_"+d.name+"_hourly", pk, hourCut, uid)
				out.Hourly += n
				if err != nil {
					return out, err
				}
			}
		}
		if minuteCut > 0 {
			if _, err := del("dnstap2_minutes", "uid, bucket, cid", minuteCut, uid); err != nil {
				return out, err
			}
		}
		if !known[uid] {
			if _, err := s.write.ExecContext(ctx, `DELETE FROM dnstap2_owners WHERE uid = ?`, uid); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

// filters builds the WHERE clause like query.go filters, with values turned
// into IDs. ok is false when a value was never stored, so nothing can match.
func (s *v2Store) filters(q Query, keys []string) (where string, args []any, ignored []string, ok bool) {
	ok = true
	uid, found := s.keyID(q.UserID)
	ok = ok && found
	conds := []string{"uid = ?"}
	args = []any{uid}
	if q.CollectorID != "" {
		cid, found := s.keyID(q.CollectorID)
		ok = ok && found
		conds = append(conds, "cid = ?")
		args = append(args, cid)
	}
	has := map[string]bool{}
	for _, k := range keys {
		has[k] = true
	}
	for _, f := range []struct{ col, val string }{{"zone", q.Zone}, {"qname", q.QName}, {"qtype", q.QType}, {"rcode", q.RCode}} {
		if f.val == "" {
			continue
		}
		if !has[f.col] {
			ignored = append(ignored, f.col)
			continue
		}
		id, found := s.keyID(f.val)
		ok = ok && found
		conds = append(conds, f.col+" = ?")
		args = append(args, id)
	}
	if q.Client.IsValid() {
		if has["net"] {
			lo, hi := netRange(q.Client)
			conds = append(conds, "net IN (SELECT id FROM dnstap2_nets WHERE s >= ? AND s < ?)")
			args = append(args, lo, hi)
		} else {
			ignored = append(ignored, "client")
		}
	}
	return strings.Join(conds, " AND "), args, ignored, ok
}

// union reads a select over the hourly and minute tables of one dimension, like query.go union.
func (s *v2Store) union(ctx context.Context, q Query, dim, sel, group string) ([][2]int64, []string, error) {
	rolled, err := s.rolledUntil(ctx)
	if err != nil {
		return nil, nil, err
	}
	hourly, minute := spans(q, rolled)
	where, args, ignored, ok := s.filters(q, dimKeys(dim))
	if !ok {
		return nil, ignored, nil
	}
	var parts []string
	var all []any
	for _, p := range []struct {
		table string
		span  [2]int64
	}{{"dnstap2_" + dim + "_hourly", hourly}, {"dnstap2_" + dim + "_minute", minute}} {
		if p.span[1] <= p.span[0] {
			continue
		}
		parts = append(parts, fmt.Sprintf(`SELECT %s FROM %s WHERE %s AND bucket >= ? AND bucket < ?`, sel, p.table, where))
		all = append(append(all, args...), p.span[0], p.span[1])
	}
	if len(parts) == 0 {
		return nil, ignored, nil
	}
	query := `SELECT k, SUM(n) FROM (` + strings.Join(parts, " UNION ALL ") + `) GROUP BY k`
	if group != "" {
		query += " " + group
	}
	rows, err := s.read.QueryContext(ctx, query, all...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out [][2]int64
	for rows.Next() {
		var r [2]int64
		if err := rows.Scan(&r[0], &r[1]); err != nil {
			return nil, nil, err
		}
		out = append(out, r)
	}
	return out, ignored, rows.Err()
}

// Dnstap answers one drill-down exactly as Store.Dnstap does.
func (s *v2Store) Dnstap(ctx context.Context, q Query, dim string, limit int) (Breakdown, error) {
	b := Breakdown{Dim: dim, Rows: []Row{}, Series: [][2]int64{}}
	source := "totals"
	switch {
	case q.Client.IsValid():
		source = "clients"
	case q.QName != "":
		source = "names"
	}
	b.Exact = source == "totals"
	col := map[string]string{"qname": "qname", "zone": "zone", "qtype": "qtype", "rcode": "rcode", "client": "net", "size": "up_to"}[dim]
	if col == "" {
		return b, fmt.Errorf("unknown dimension %q", dim)
	}
	table := source
	switch dim {
	case "qname":
		if source != "clients" {
			table = "names"
		}
	case "client":
		table = "clients"
	case "size":
		table = "sizes"
	}
	rows, ignored, err := s.union(ctx, q, table, col+" AS k, count AS n", "")
	if err != nil {
		return b, err
	}
	b.Ignored = ignored
	for _, r := range rows {
		var key string
		switch dim {
		case "client":
			key = netLabel(s.netKeyStr(r[0]))
		case "size":
			key = strconv.FormatInt(r[0], 10)
		default:
			key = s.label(r[0])
		}
		if (table == "names" || table == "clients") && dim == "qname" && key == wire.Other {
			b.Other += r[1]
			continue
		}
		b.Rows = append(b.Rows, Row{Key: key, Count: r[1]})
	}
	sort.Slice(b.Rows, func(i, j int) bool {
		if b.Rows[i].Count != b.Rows[j].Count {
			return b.Rows[i].Count > b.Rows[j].Count
		}
		return b.Rows[i].Key < b.Rows[j].Key
	})
	if dim == "size" {
		sort.Slice(b.Rows, func(i, j int) bool { return sizeOrder(b.Rows[i].Key) < sizeOrder(b.Rows[j].Key) })
	}
	if limit > 0 && len(b.Rows) > limit {
		for _, r := range b.Rows[limit:] {
			b.Other += r.Count
		}
		b.Rows = b.Rows[:limit]
	}
	if dim == "qname" || source != "totals" {
		ft := "names"
		if source == "clients" {
			ft = "clients"
		}
		fq := q
		fq.QName, fq.QType, fq.RCode = wire.Other, "", ""
		folded, _, err := s.union(ctx, fq, ft, "1 AS k, count AS n", "")
		if err != nil {
			return b, err
		}
		b.Approximate = len(folded) > 0 && folded[0][1] > 0
	}
	step := max(q.Step, minuteMs)
	srows, _, err := s.union(ctx, q, source, fmt.Sprintf("(bucket / %d) * %d AS k, count AS n", step, step), "ORDER BY k")
	if err != nil {
		return b, err
	}
	for _, r := range srows {
		b.Total += r[1]
		b.Series = append(b.Series, r)
	}
	b.Series = zeroFill(b.Series, q.Start, q.End, step)
	if uid, ok := s.keyID(q.UserID); ok {
		where := "uid = ? AND bucket >= ? AND bucket < ? AND queries_only = 1"
		args := []any{uid, q.Start, q.End}
		if q.CollectorID != "" {
			cid, _ := s.keyID(q.CollectorID)
			where += " AND cid = ?"
			args = append(args, cid)
		}
		var qo int
		if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM dnstap2_minutes WHERE `+where, args...).Scan(&qo); err != nil {
			return b, err
		}
		b.QueriesOnly = qo > 0
	}
	return b, nil
}

func (s *v2Store) reportOther(ctx context.Context, user string) (map[string]int64, error) {
	out := map[string]int64{}
	uid, _ := s.keyID(user)
	other, _ := s.keyID(wire.Other)
	for _, table := range []string{"dnstap2_names_minute", "dnstap2_clients_minute"} {
		var n int64
		if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE uid = ? AND qname = ?`, uid, other).Scan(&n); err != nil {
			return nil, err
		}
		out[table] = n
	}
	return out, nil
}

func (s *v2Store) reportFocus(ctx context.Context, user string, since int64) (zone, qname, netKey string) {
	uid, _ := s.keyID(user)
	other, _ := s.keyID(wire.Other)
	var z, q, n sql.NullInt64
	s.read.QueryRowContext(ctx, `SELECT zone FROM dnstap2_totals_minute WHERE uid = ? AND bucket >= ? GROUP BY zone ORDER BY SUM(count) DESC LIMIT 1`, uid, since).Scan(&z)
	s.read.QueryRowContext(ctx, `SELECT qname FROM dnstap2_names_minute WHERE uid = ? AND bucket >= ? AND qname <> ? GROUP BY qname ORDER BY SUM(count) DESC LIMIT 1`, uid, since, other).Scan(&q)
	s.read.QueryRowContext(ctx, `SELECT net FROM dnstap2_clients_minute WHERE uid = ? AND bucket >= ? GROUP BY net ORDER BY SUM(count) DESC LIMIT 1`, uid, since).Scan(&n)
	if z.Valid {
		zone = s.label(z.Int64)
	}
	if q.Valid {
		qname = s.label(q.Int64)
	}
	if n.Valid {
		netKey = s.netKeyStr(n.Int64)
	}
	return zone, qname, netKey
}

// TestV2MatchesStore feeds the same minutes to Store and v2Store and checks
// every drill-down the report times gives the same answer from both.
func TestV2MatchesStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cur := openTraced(t, filepath.Join(dir, "current.db"))
	defer cur.Close()
	v2 := openV2(t, filepath.Join(dir, "v2.db"))
	defer v2.Close()
	// A small rollup cap, so the hourly fold has work to do.
	cur.TopHourly, v2.TopHourly = 100, 100

	g := newGenerator(1, reportCfg{Zones: 5, Names: 2000, Nets: 400, Fold: "subnet"})
	const user, col = "5b0c8a2e-3f6d-4c1b-9e7a-2d4f6a8c0e11", "9d3e7f10-0000-4a5b-8c6d-0e1f2a3b4c5d"
	end := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).UnixMilli()
	first := end - 3*hourMs
	for m := first; m < end; m += minuteMs {
		d := g.sample(3000).bucket(m, 50, 20, 50, 1)
		d.QueriesOnly = m == first+30*minuteMs
		if err := cur.SaveDnstap(ctx, user, col, d, m+minuteMs); err != nil {
			t.Fatal(err)
		}
		if err := v2.SaveDnstap(ctx, user, col, d, m+minuteMs); err != nil {
			t.Fatal(err)
		}
	}
	zone, qname, netKey := cur.reportFocus(ctx, user, first)
	if z, q, n := v2.reportFocus(ctx, user, first); z != zone || q != qname || n != netKey {
		t.Fatalf("focus differs: current %s %s %s, v2 %s %s %s", zone, qname, netKey, z, q, n)
	}
	if zone == "" || qname == "" || netKey == "" {
		t.Fatalf("no focus values: %q %q %q", zone, qname, netKey)
	}
	p, err := netip.ParsePrefix(netLabel(netKey))
	if err != nil {
		t.Fatal(err)
	}
	client := netip.PrefixFrom(p.Addr(), 16).Masked()
	if p.Addr().Is6() {
		client = netip.PrefixFrom(p.Addr(), 32).Masked()
	}
	a, err := cur.reportOther(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	b, err := v2.reportOther(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if a["dnstap_names_minute"] != b["dnstap2_names_minute"] || a["dnstap_clients_minute"] != b["dnstap2_clients_minute"] {
		t.Errorf("(other) rows differ: current %v, v2 %v", a, b)
	}

	type filter struct {
		name string
		set  func(*Query)
	}
	filtersList := []filter{
		{"none", func(*Query) {}},
		{"zone", func(q *Query) { q.Zone = zone }},
		{"qname", func(q *Query) { q.QName = qname }},
		{"client", func(q *Query) { q.Client = client }},
		{"collector+rcode", func(q *Query) { q.CollectorID = col; q.RCode = "NXDOMAIN" }},
		{"missing zone", func(q *Query) { q.Zone = "nowhere.example." }},
	}
	allDims := []string{"qname", "zone", "qtype", "rcode", "client", "size"}
	// compare reports how many cases had traffic and how many had folded rows,
	// so a pass is known to have compared real data.
	compare := func(label string, base Query, flist []filter, dimsList []string) {
		withData, withOther := 0, 0
		defer func() {
			t.Logf("%s: %d cases with traffic, %d with (other) rows", label, withData, withOther)
			if withData == 0 || withOther == 0 {
				t.Errorf("%s compared no traffic or no folded rows", label)
			}
		}()
		for _, f := range flist {
			for _, dim := range dimsList {
				q := base
				f.set(&q)
				want, err := cur.Dnstap(ctx, q, dim, 50)
				if err != nil {
					t.Fatal(err)
				}
				got, err := v2.Dnstap(ctx, q, dim, 50)
				if err != nil {
					t.Fatal(err)
				}
				if want.Total > 0 {
					withData++
				}
				if want.Other > 0 {
					withOther++
				}
				if !reflect.DeepEqual(want, got) {
					t.Errorf("%s %s by %s differs:\ncurrent total %d other %d rows %v flags %v/%v/%v ignored %v\nv2      total %d other %d rows %v flags %v/%v/%v ignored %v",
						label, f.name, dim,
						want.Total, want.Other, want.Rows, want.Approximate, want.Exact, want.QueriesOnly, want.Ignored,
						got.Total, got.Other, got.Rows, got.Approximate, got.Exact, got.QueriesOnly, got.Ignored)
				}
			}
		}
	}
	// Minute windows: the start is within minute retention, so both read minutes only.
	for _, span := range []int64{hourMs, 2 * hourMs} {
		compare(fmt.Sprintf("minutes %dh", span/hourMs), Query{UserID: user, Start: end - span, End: end, Step: 5 * minuteMs, MinuteFrom: end - 7*dayMs}, filtersList, allDims)
	}

	// Rollup: both roll the one settled hour the same way.
	n1, err := cur.Rollup(ctx, end)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := v2.Rollup(ctx, end)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 1 || n2 != 1 {
		t.Fatalf("rolled hours: current %d, v2 %d, want 1", n1, n2)
	}
	// Hourly reads: MinuteFrom past the start forces the hourly tables. Client
	// rows are left out, since the current rollup folds them by zone, qtype and rcode.
	hourly := Query{UserID: user, Start: first, End: first + hourMs, Step: hourMs, MinuteFrom: end}
	compare("hourly", hourly, filtersList[:3], []string{"qname", "zone", "qtype", "rcode", "size"})
	// A window over the rolled hour and the minutes after it reads both tables.
	compare("hourly+minutes", Query{UserID: user, Start: first, End: end, Step: hourMs, MinuteFrom: end}, filtersList[:3], []string{"qname", "zone", "qtype", "rcode", "size"})

	// Prune: one hour of minutes past retention from each, then compare what is left.
	r1, err := cur.Prune(ctx, Retention{MinuteMs: 2 * hourMs}, end, []string{user})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := v2.Prune(ctx, Retention{MinuteMs: 2 * hourMs}, end, []string{user})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Minute != r2.Minute || r1.Minute == 0 {
		t.Errorf("pruned minute rows: current %d, v2 %d", r1.Minute, r2.Minute)
	}
	compare("after prune", Query{UserID: user, Start: end - 2*hourMs, End: end, Step: 5 * minuteMs, MinuteFrom: end - 7*dayMs}, filtersList, allDims)
}
