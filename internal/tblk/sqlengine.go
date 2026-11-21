package tblk

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// sqlEngine keeps every tier in one SQLite file. Strings live once in a
// dictionary (keys; networks apart in nets, so a CIDR is a range of their sorted
// text); rows are all integers with the primary key (owner, bucket, collector,
// keys…), so any window of an owner is one range of the key.
type sqlEngine struct {
	sch         *schemaInfo
	path        string
	grains      []Grain
	write, read *sql.DB

	// wmu serialises writes, so a cached ID is never swept between lookup and use.
	wmu sync.Mutex
	// The ID caches are bounded; reads that miss ask the dictionary.
	cmu  sync.RWMutex
	keys map[string]int64
	nets map[string]int64
}

// cacheMax bounds each ID cache; past it the cache starts over.
const cacheMax = 200_000

func sqlDSN(path string) string {
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "synchronous(NORMAL)", "busy_timeout(5000)"} {
		q.Add("_pragma", p)
	}
	return "file:" + path + "?" + q.Encode()
}

func tableOf(f *famInfo, g Grain) string { return "t_" + f.Name + "_" + g.String() }
func keyCol(k Key) string                { return "k_" + k.Name }
func msrCol(m string) string             { return "m_" + m }

func (e *sqlEngine) keyCols(f *famInfo) []string {
	out := make([]string, len(f.Keys))
	for i, k := range f.Keys {
		out[i] = keyCol(k)
	}
	return out
}

func (e *sqlEngine) msrCols(f *famInfo) []string {
	out := make([]string, len(f.Measures))
	for i, m := range f.Measures {
		out[i] = msrCol(m)
	}
	return out
}

func (e *sqlEngine) pk(f *famInfo) string {
	return strings.Join(append([]string{"owner", "bucket", "collector"}, e.keyCols(f)...), ", ")
}

func openSQL(path string, si *schemaInfo, driver string, gs []Grain) (*sqlEngine, error) {
	w, err := sql.Open(driver, sqlDSN(path))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open(driver, sqlDSN(path))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	e := &sqlEngine{sch: si, path: path, grains: gs, write: w, read: r, keys: map[string]int64{}, nets: map[string]int64{}}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS keys (id INTEGER PRIMARY KEY, s TEXT NOT NULL UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS nets (id INTEGER PRIMARY KEY, s TEXT NOT NULL UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS owners (owner INTEGER PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS rolled_days (day INTEGER PRIMARY KEY)`,
	}
	for _, f := range si.fams {
		cols := ""
		for _, c := range e.keyCols(f) {
			cols += c + " INTEGER NOT NULL, "
		}
		for _, c := range e.msrCols(f) {
			cols += c + " INTEGER NOT NULL, "
		}
		for _, g := range gs {
			stmts = append(stmts, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (owner INTEGER NOT NULL, bucket INTEGER NOT NULL, collector INTEGER NOT NULL, %sPRIMARY KEY (%s)) WITHOUT ROWID`,
				tableOf(f, g), cols, e.pk(f)))
		}
	}
	for _, s := range stmts {
		if _, err := w.Exec(s); err != nil {
			e.close()
			return nil, fmt.Errorf("tblk schema: %w", err)
		}
	}
	// Other exists from the start, so folded-row filters always resolve.
	ctx := context.Background()
	tx, err := e.begin(ctx)
	if err != nil {
		e.close()
		return nil, err
	}
	if _, err := tx.key(ctx, Other); err != nil {
		tx.rollback()
		e.close()
		return nil, err
	}
	if err := checkShapes(ctx, tx.tx, si); err != nil {
		tx.rollback()
		e.close()
		return nil, err
	}
	if err := tx.commit(); err != nil {
		e.close()
		return nil, err
	}
	return e, nil
}

// ErrShapeChanged is returned by Open when a family's declared keys,
// measures or folding differ from what the store was written with: blocks
// do not carry the schema, so old rows would be read under the new shape.
var ErrShapeChanged = errors.New("tblk: a family changed shape since the store was written; declare the new shape under a new family name")

// shapeOf is a family's fingerprint: everything that decides how its stored
// rows read (key names, kinds and folding in order, measures in order, the
// bound measure). Caps may change freely and are left out.
func shapeOf(f *famInfo) int64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s|", f.Name)
	for _, k := range f.Keys {
		fmt.Fprintf(h, "k:%s:%d:%t:%d|", k.Name, k.Kind, k.Fold, k.Level)
	}
	for _, m := range f.Measures {
		fmt.Fprintf(h, "m:%s|", m)
	}
	fmt.Fprintf(h, "b:%s", f.Bound)
	return int64(h.Sum64())
}

// checkShapes records each family's fingerprint the first time and refuses
// a store whose recorded fingerprint differs. Families no longer declared
// are left alone.
func checkShapes(ctx context.Context, tx *sql.Tx, si *schemaInfo) error {
	for _, f := range si.fams {
		key, want := "shape:"+f.Name, shapeOf(f)
		var got int64
		switch err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&got); {
		case err == sql.ErrNoRows:
			if err := metaSet(ctx, tx, key, want); err != nil {
				return err
			}
		case err != nil:
			return err
		case got != want:
			return fmt.Errorf("%w (family %q)", ErrShapeChanged, f.Name)
		}
	}
	return nil
}

func (e *sqlEngine) close() error {
	e.read.Close()
	return e.write.Close()
}

func (e *sqlEngine) has(g Grain) bool {
	for _, x := range e.grains {
		if x == g {
			return true
		}
	}
	return false
}

// sqlTx is a write transaction that interns strings; new IDs reach the caches
// only once it commits.
type sqlTx struct {
	e                *sqlEngine
	tx               *sql.Tx
	keyStmt, netStmt *sql.Stmt
	newKeys, newNets map[string]int64
}

func (e *sqlEngine) begin(ctx context.Context) (*sqlTx, error) {
	tx, err := e.write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &sqlTx{e: e, tx: tx, newKeys: map[string]int64{}, newNets: map[string]int64{}}, nil
}

func (x *sqlTx) intern(ctx context.Context, v, table string, stmt **sql.Stmt, cache, fresh map[string]int64) (int64, error) {
	x.e.cmu.RLock()
	id, ok := cache[v]
	x.e.cmu.RUnlock()
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

func (x *sqlTx) key(ctx context.Context, v string) (int64, error) {
	return x.intern(ctx, v, "keys", &x.keyStmt, x.e.keys, x.newKeys)
}

func (x *sqlTx) net(ctx context.Context, v string) (int64, error) {
	return x.intern(ctx, v, "nets", &x.netStmt, x.e.nets, x.newNets)
}

// encode turns one key value into its column value.
func (x *sqlTx) encode(ctx context.Context, k Key, v string) (int64, error) {
	switch k.Kind {
	case KeyNet:
		return x.net(ctx, v)
	case KeyInt:
		return strconv.ParseInt(v, 10, 64)
	}
	return x.key(ctx, v)
}

func (x *sqlTx) commit() error {
	if err := x.tx.Commit(); err != nil {
		return err
	}
	x.e.cmu.Lock()
	for _, l := range []struct{ fresh, cache map[string]int64 }{{x.newKeys, x.e.keys}, {x.newNets, x.e.nets}} {
		if len(l.cache)+len(l.fresh) > cacheMax {
			clear(l.cache)
		}
		for v, id := range l.fresh {
			l.cache[v] = id
		}
	}
	x.e.cmu.Unlock()
	return nil
}

func (x *sqlTx) rollback() { x.tx.Rollback() }

// put writes rows into one table, replacing rows with the same key, or with
// sum, adding to them.
func (x *sqlTx) put(ctx context.Context, f *famInfo, g Grain, owner int64, rows []rec, sum bool) error {
	if len(rows) == 0 {
		return nil
	}
	e := x.e
	kc, mc := e.keyCols(f), e.msrCols(f)
	cols := append(append([]string{"owner", "bucket", "collector"}, kc...), mc...)
	sets := make([]string, len(mc))
	for i, c := range mc {
		sets[i] = c + " = excluded." + c
		if sum {
			sets[i] = c + " = " + c + " + excluded." + c
		}
	}
	set := "NOTHING"
	if len(sets) > 0 {
		set = "UPDATE SET " + strings.Join(sets, ", ")
	}
	stmt, err := x.tx.PrepareContext(ctx, fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO %s`,
		tableOf(f, g), strings.Join(cols, ", "), strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", "), e.pk(f), set))
	if err != nil {
		return err
	}
	defer stmt.Close()
	args := make([]any, 0, len(cols))
	colIDs := map[string]int64{}
	for _, r := range rows {
		cid, ok := colIDs[r.collector]
		if !ok {
			if cid, err = x.key(ctx, r.collector); err != nil {
				return err
			}
			colIDs[r.collector] = cid
		}
		args = append(args[:0], owner, r.bucket, cid)
		for i, k := range f.Keys {
			v, err := x.encode(ctx, k, r.keys[i])
			if err != nil {
				return err
			}
			args = append(args, v)
		}
		for _, v := range r.vals {
			args = append(args, v)
		}
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return err
		}
	}
	return nil
}

func (e *sqlEngine) append(ctx context.Context, owner, collector string, f *famInfo, g Grain, bucket int64, rows []rec) error {
	if !e.has(g) {
		return fmt.Errorf("tblk: this store keeps no %s tier", g)
	}
	e.wmu.Lock()
	defer e.wmu.Unlock()
	x, err := e.begin(ctx)
	if err != nil {
		return err
	}
	defer x.rollback()
	oid, err := x.key(ctx, owner)
	if err != nil {
		return err
	}
	if _, err := x.tx.ExecContext(ctx, `INSERT INTO owners (owner) VALUES (?) ON CONFLICT DO NOTHING`, oid); err != nil {
		return err
	}
	if err := x.put(ctx, f, g, oid, rows, false); err != nil {
		return err
	}
	return x.commit()
}

// add sums minute rows in. An hour already rolled, or a day, gets the rows
// summed too: the rollup will not read those minutes again.
func (e *sqlEngine) add(ctx context.Context, owner, collector string, f *famInfo, bucket int64, rows []rec) error {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	x, err := e.begin(ctx)
	if err != nil {
		return err
	}
	defer x.rollback()
	oid, err := x.key(ctx, owner)
	if err != nil {
		return err
	}
	if _, err := x.tx.ExecContext(ctx, `INSERT INTO owners (owner) VALUES (?) ON CONFLICT DO NOTHING`, oid); err != nil {
		return err
	}
	if err := x.put(ctx, f, Minute, oid, rows, true); err != nil {
		return err
	}
	if e.has(Hour) {
		h := bucket - bucket%hourMs
		rolled, err := e.metaGet(ctx, x.tx, "hour_rolled")
		if err != nil {
			return err
		}
		if h < rolled {
			if err := x.put(ctx, f, Hour, oid, sumRecs(rows, h), true); err != nil {
				return err
			}
		}
	}
	if e.has(Day) {
		d := bucket - bucket%dayMs
		var n int
		if err := x.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM rolled_days WHERE day = ?`, d).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			if err := x.put(ctx, f, Day, oid, sumRecs(rows, d), true); err != nil {
				return err
			}
		}
	}
	return x.commit()
}

func (e *sqlEngine) flushTier(context.Context) error { return nil }

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (e *sqlEngine) metaGet(ctx context.Context, q queryer, key string) (int64, error) {
	var v int64
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

func metaSet(ctx context.Context, tx *sql.Tx, key string, v int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, v)
	return err
}

func (e *sqlEngine) rolled(ctx context.Context) (int64, int64, error) {
	h, err := e.metaGet(ctx, e.read, "hour_rolled")
	if err != nil {
		return 0, 0, err
	}
	d, err := e.metaGet(ctx, e.read, "day_rolled")
	return h, d, err
}

type owner struct {
	id    int64
	label string
}

func (e *sqlEngine) ownerList(ctx context.Context) ([]string, error) {
	list, err := e.owners(ctx, e.read)
	out := make([]string, len(list))
	for i, o := range list {
		out[i] = o.label
	}
	return out, err
}

func (e *sqlEngine) owners(ctx context.Context, q queryer) ([]owner, error) {
	rows, err := q.QueryContext(ctx, `SELECT o.owner, k.s FROM owners o JOIN keys k ON k.id = o.owner ORDER BY k.s`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []owner
	for rows.Next() {
		var o owner
		if err := rows.Scan(&o.id, &o.label); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// labelJoin returns the select list and joins that turn a subquery t's key
// columns back into stored strings.
func (e *sqlEngine) labelJoin(f *famInfo) (sel []string, joins string) {
	for i, k := range f.Keys {
		c := keyCol(k)
		a := fmt.Sprintf("d%d", i)
		switch k.Kind {
		case KeyInt:
			sel = append(sel, "t."+c)
		case KeyNet:
			sel = append(sel, a+".s")
			joins += fmt.Sprintf(" JOIN nets %s ON %s.id = t.%s", a, a, c)
		default:
			sel = append(sel, a+".s")
			joins += fmt.Sprintf(" JOIN keys %s ON %s.id = t.%s", a, a, c)
		}
	}
	return sel, joins
}

// dump reads an owner's rows of [a, b) as stored strings, summed per collector
// and keys (and per bucket when keepBucket).
func (e *sqlEngine) dump(ctx context.Context, q queryer, f *famInfo, g Grain, ownerID, a, b int64, keepBucket bool) ([]rec, error) {
	kc, mc := e.keyCols(f), e.msrCols(f)
	group := append([]string{"collector"}, kc...)
	if keepBucket {
		group = append(group, "bucket")
	}
	sums := make([]string, len(mc))
	for i, c := range mc {
		sums[i] = "SUM(" + c + ") AS " + c
	}
	inner := fmt.Sprintf(`SELECT %s, %s FROM %s WHERE owner = ? AND bucket >= ? AND bucket < ? GROUP BY %s`,
		strings.Join(group, ", "), strings.Join(sums, ", "), tableOf(f, g), strings.Join(group, ", "))
	ksel, joins := e.labelJoin(f)
	sel := append(append([]string{"c.s"}, ksel...), "t.bucket")
	if !keepBucket {
		sel[len(sel)-1] = "0"
	}
	for _, c := range mc {
		sel = append(sel, "t."+c)
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM (%s) t JOIN keys c ON c.id = t.collector%s`, strings.Join(sel, ", "), inner, joins), ownerID, a, b)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// A sealed hour is hundreds of thousands of rows over a few thousand
	// distinct labels: intern the labels, and give rows their keys and
	// values from shared slabs instead of two allocations per row.
	nk, nm := len(kc), len(mc)
	intern := map[string]string{}
	var col string
	keys := make([]string, nk)
	dest := []any{&col}
	for i := range keys {
		dest = append(dest, &keys[i])
	}
	var bucket int64
	dest = append(dest, &bucket)
	vals := make([]int64, nm)
	for i := range vals {
		dest = append(dest, &vals[i])
	}
	const slabRows = 4096
	var keySlab []string
	var valSlab []int64
	var out []rec
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if len(keySlab) < nk || len(valSlab) < nm {
			keySlab, valSlab = make([]string, slabRows*nk), make([]int64, slabRows*nm)
		}
		r := rec{collector: internStr(intern, col), bucket: bucket, keys: keySlab[:nk:nk], vals: valSlab[:nm:nm]}
		keySlab, valSlab = keySlab[nk:], valSlab[nm:]
		for i, k := range keys {
			r.keys[i] = internStr(intern, k)
		}
		copy(r.vals, vals)
		out = append(out, r)
	}
	return out, rows.Err()
}

func internStr(m map[string]string, s string) string {
	if v, ok := m[s]; ok {
		return v
	}
	m[s] = s
	return s
}

// rollBucket builds bucket [a, a+to.Ms()) of tier to from tier from, for every owner.
func (e *sqlEngine) rollBucket(ctx context.Context, x *sqlTx, from, to Grain, a int64) error {
	owners, err := e.owners(ctx, x.tx)
	if err != nil {
		return err
	}
	for _, o := range owners {
		for _, f := range e.sch.fams {
			rows, err := e.dump(ctx, x.tx, f, from, o.id, a, a+to.Ms(), false)
			if err != nil {
				return err
			}
			if _, err := x.tx.ExecContext(ctx, `DELETE FROM `+tableOf(f, to)+` WHERE owner = ? AND bucket = ?`, o.id, a); err != nil {
				return err
			}
			if err := x.put(ctx, f, to, o.id, rollRows(f, to, a, rows), false); err != nil {
				return err
			}
		}
	}
	return nil
}

// minBucket is the oldest bucket of a tier across owners and families.
func (e *sqlEngine) minBucket(ctx context.Context, q queryer, g Grain) (int64, bool, error) {
	owners, err := e.owners(ctx, q)
	if err != nil {
		return 0, false, err
	}
	found, best := false, int64(0)
	for _, o := range owners {
		for _, f := range e.sch.fams {
			var v sql.NullInt64
			if err := q.QueryRowContext(ctx, `SELECT MIN(bucket) FROM `+tableOf(f, g)+` WHERE owner = ?`, o.id).Scan(&v); err != nil {
				return 0, false, err
			}
			if v.Valid && (!found || v.Int64 < best) {
				found, best = true, v.Int64
			}
		}
	}
	return best, found, nil
}

func (e *sqlEngine) maintain(ctx context.Context, now int64, limit int) (MaintainResult, error) {
	var res MaintainResult
	e.wmu.Lock()
	defer e.wmu.Unlock()
	if e.has(Hour) {
		until, err := e.metaGet(ctx, e.read, "hour_rolled")
		if err != nil {
			return res, err
		}
		if until == 0 {
			oldest, ok, err := e.minBucket(ctx, e.read, Minute)
			if err != nil {
				return res, err
			}
			if ok {
				until = oldest - oldest%hourMs
			}
		}
		target := now - rollLag
		target -= target % hourMs
		for h := until; until > 0 && h < target && res.Hours < limit && ctx.Err() == nil; h += hourMs {
			x, err := e.begin(ctx)
			if err != nil {
				return res, err
			}
			if err := e.rollBucket(ctx, x, Minute, Hour, h); err != nil {
				x.rollback()
				return res, err
			}
			if err := metaSet(ctx, x.tx, "hour_rolled", h+hourMs); err != nil {
				x.rollback()
				return res, err
			}
			if err := x.commit(); err != nil {
				return res, err
			}
			res.Hours++
		}
	}
	if e.has(Day) {
		hourRolled, err := e.metaGet(ctx, e.read, "hour_rolled")
		if err != nil {
			return res, err
		}
		first, ok, err := e.minBucket(ctx, e.read, Hour)
		if err != nil {
			return res, err
		}
		done := map[int64]bool{}
		rows, err := e.read.QueryContext(ctx, `SELECT day FROM rolled_days`)
		if err != nil {
			return res, err
		}
		for rows.Next() {
			var d int64
			rows.Scan(&d)
			done[d] = true
		}
		rows.Close()
		// A day rolls once every hour of it is in the hour tier: before
		// hour_rolled, or older history imported straight into hours.
		reach := max(hourRolled, 0)
		for d := first - first%dayMs; ok && d+dayMs <= reach && res.Days < limit && ctx.Err() == nil; d += dayMs {
			if done[d] {
				continue
			}
			x, err := e.begin(ctx)
			if err != nil {
				return res, err
			}
			if err := e.rollBucket(ctx, x, Hour, Day, d); err != nil {
				x.rollback()
				return res, err
			}
			if _, err := x.tx.ExecContext(ctx, `INSERT INTO rolled_days (day) VALUES (?) ON CONFLICT DO NOTHING`, d); err != nil {
				x.rollback()
				return res, err
			}
			cur, err := e.metaGet(ctx, x.tx, "day_rolled")
			if err != nil {
				x.rollback()
				return res, err
			}
			if err := metaSet(ctx, x.tx, "day_rolled", max(cur, d+dayMs)); err != nil {
				x.rollback()
				return res, err
			}
			if err := x.commit(); err != nil {
				return res, err
			}
			res.Days++
		}
	}
	last, err := e.metaGet(ctx, e.read, "last_sweep")
	if err != nil {
		return res, err
	}
	if now-last >= dayMs {
		if err := e.sweep(ctx, now); err != nil {
			return res, err
		}
	}
	return res, nil
}

// sweep deletes dictionary strings no row references any more.
func (e *sqlEngine) sweep(ctx context.Context, now int64) error {
	var keyRefs, netRefs []string
	keyRefs = append(keyRefs, `SELECT owner FROM owners`)
	for _, f := range e.sch.fams {
		for _, g := range e.grains {
			t := tableOf(f, g)
			keyRefs = append(keyRefs, `SELECT owner FROM `+t, `SELECT collector FROM `+t)
			for _, k := range f.Keys {
				switch k.Kind {
				case KeyString:
					keyRefs = append(keyRefs, `SELECT `+keyCol(k)+` FROM `+t)
				case KeyNet:
					netRefs = append(netRefs, `SELECT `+keyCol(k)+` FROM `+t)
				}
			}
		}
	}
	tx, err := e.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM keys WHERE s <> ? AND id NOT IN (`+strings.Join(keyRefs, " UNION ")+`)`, Other); err != nil {
		return err
	}
	if len(netRefs) > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM nets WHERE id NOT IN (`+strings.Join(netRefs, " UNION ")+`)`); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `DELETE FROM nets`); err != nil {
		return err
	}
	if err := metaSet(ctx, tx, "last_sweep", now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	e.cmu.Lock()
	clear(e.keys)
	clear(e.nets)
	e.cmu.Unlock()
	return nil
}

func (e *sqlEngine) deleteBatches(ctx context.Context, query string, args ...any) (int64, error) {
	const batch = 5000
	var total int64
	for {
		res, err := e.write.ExecContext(ctx, query, append(args, batch)...)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < batch || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

// deleteBefore removes every owner's rows of tier g older than cut.
func (e *sqlEngine) deleteBefore(ctx context.Context, g Grain, cut int64) (int64, error) {
	owners, err := e.owners(ctx, e.read)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, o := range owners {
		for _, f := range e.sch.fams {
			t, pk := tableOf(f, g), e.pk(f)
			d, err := e.deleteBatches(ctx, fmt.Sprintf(`DELETE FROM %s WHERE (%s) IN (SELECT %s FROM %s WHERE owner = ? AND bucket < ? LIMIT ?)`, t, pk, pk, t), o.id, cut)
			n += d
			if err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// usedBytes is the file's size less its free pages.
func (e *sqlEngine) usedBytes(ctx context.Context) (int64, error) {
	var pages, free, size int64
	for _, p := range []struct {
		q string
		v *int64
	}{{"PRAGMA page_count", &pages}, {"PRAGMA freelist_count", &free}, {"PRAGMA page_size", &size}} {
		if err := e.read.QueryRowContext(ctx, p.q).Scan(p.v); err != nil {
			return 0, err
		}
	}
	return (pages - free) * size, nil
}

func (e *sqlEngine) prune(ctx context.Context, now int64, keep map[Grain]time.Duration) (PruneResult, error) {
	res := PruneResult{Rows: map[Grain]int64{}, Files: map[Grain]int{}}
	e.wmu.Lock()
	defer e.wmu.Unlock()
	for _, g := range e.grains {
		d := keep[g]
		if d <= 0 {
			continue
		}
		cut := now - d.Milliseconds()
		n, err := e.deleteBefore(ctx, g, cut)
		res.Rows[g] += n
		if err != nil {
			return res, err
		}
		if g == Day {
			if _, err := e.write.ExecContext(ctx, `DELETE FROM rolled_days WHERE day < ?`, cut); err != nil {
				return res, err
			}
		}
	}
	if e.sch.MaxBytes <= 0 {
		return res, nil
	}
	// Over budget: drop the oldest, most detailed data first. Minutes go once
	// rolled into hours, hours once rolled into days, then the oldest days.
	hourRolled, dayRolled, err := e.rolled(ctx)
	if err != nil {
		return res, err
	}
	for ctx.Err() == nil {
		used, err := e.usedBytes(ctx)
		if err != nil || used <= e.sch.MaxBytes {
			return res, err
		}
		dropped := false
		for _, c := range []struct {
			g     Grain
			unit  int64
			limit int64
		}{{Minute, hourMs, hourRolled}, {Hour, dayMs, dayRolled}, {Day, dayMs, now}} {
			if !e.has(c.g) {
				continue
			}
			oldest, ok, err := e.minBucket(ctx, e.read, c.g)
			if err != nil {
				return res, err
			}
			cut := oldest - oldest%c.unit + c.unit
			if !ok || cut > c.limit {
				continue
			}
			n, err := e.deleteBefore(ctx, c.g, cut)
			res.Rows[c.g] += n
			if err != nil {
				return res, err
			}
			dropped = true
			break
		}
		if !dropped {
			return res, nil
		}
	}
	return res, ctx.Err()
}

func (e *sqlEngine) dropOwner(ctx context.Context, ownerLabel string) error {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	id, ok, err := e.lookup(ctx, "keys", ownerLabel)
	if err != nil || !ok {
		return err
	}
	for _, f := range e.sch.fams {
		for _, g := range e.grains {
			t, pk := tableOf(f, g), e.pk(f)
			if _, err := e.deleteBatches(ctx, fmt.Sprintf(`DELETE FROM %s WHERE (%s) IN (SELECT %s FROM %s WHERE owner = ? LIMIT ?)`, t, pk, pk, t), id); err != nil {
				return err
			}
		}
	}
	_, err = e.write.ExecContext(ctx, `DELETE FROM owners WHERE owner = ?`, id)
	return err
}

// lookup finds a string's ID without adding it.
func (e *sqlEngine) lookup(ctx context.Context, table, v string) (int64, bool, error) {
	cache := e.keys
	if table == "nets" {
		cache = e.nets
	}
	e.cmu.RLock()
	id, ok := cache[v]
	e.cmu.RUnlock()
	if ok {
		return id, true, nil
	}
	err := e.read.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE s = ?`, v).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return id, err == nil, err
}

// where builds the conditions of a scan; ok is false when a value was never
// stored, so nothing can match.
func (e *sqlEngine) where(ctx context.Context, sp scanSpec) (string, []any, bool, error) {
	oid, ok, err := e.lookup(ctx, "keys", sp.owner)
	if err != nil || !ok {
		return "", nil, false, err
	}
	conds := []string{"owner = ?", "bucket >= ?", "bucket < ?"}
	args := []any{oid, sp.start, sp.end}
	if sp.collector != "" {
		cid, ok, err := e.lookup(ctx, "keys", sp.collector)
		if err != nil || !ok {
			return "", nil, false, err
		}
		conds = append(conds, "collector = ?")
		args = append(args, cid)
	}
	for _, f := range sp.eq {
		col, table := "collector", "keys"
		var kind KeyKind = KeyString
		if f.key >= 0 {
			k := sp.fam.Keys[f.key]
			col, kind = keyCol(k), k.Kind
			if kind == KeyNet {
				table = "nets"
			}
		}
		var ids []any
		for _, v := range f.values {
			if kind == KeyInt {
				n, _ := strconv.ParseInt(v, 10, 64)
				ids = append(ids, n)
				continue
			}
			id, ok, err := e.lookup(ctx, table, v)
			if err != nil {
				return "", nil, false, err
			}
			if ok {
				ids = append(ids, id)
			}
		}
		switch {
		case len(ids) == 0 && f.not:
			continue // nothing stored to exclude
		case len(ids) == 0:
			return "", nil, false, nil // nothing stored can match
		}
		op := " IN ("
		if f.not {
			op = " NOT IN ("
		}
		conds = append(conds, col+op+strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")+")")
		args = append(args, ids...)
	}
	for _, r := range sp.rng {
		conds = append(conds, keyCol(sp.fam.Keys[r.key])+" IN (SELECT id FROM nets WHERE s >= ? AND s < ?)")
		args = append(args, r.lo, r.hi)
	}
	return strings.Join(conds, " AND "), args, true, nil
}

func (e *sqlEngine) scan(ctx context.Context, sp scanSpec, out *partial) error {
	if !e.has(sp.grain) || sp.end <= sp.start {
		return nil
	}
	where, args, ok, err := e.where(ctx, sp)
	if err != nil || !ok {
		return err
	}
	f := sp.fam
	t := tableOf(f, sp.grain)
	sums := make([]string, len(sp.msr))
	for i, m := range sp.msr {
		sums[i] = "SUM(" + msrCol(f.Measures[m]) + ")"
	}
	vals := make([]int64, len(sp.msr))
	scanVals := func(rows *sql.Rows, head ...any) error {
		dest := head
		for i := range vals {
			dest = append(dest, &vals[i])
		}
		return rows.Scan(dest...)
	}
	if sp.by != -1 {
		c, kind := "collector", KeyString
		if sp.by >= 0 {
			k := f.Keys[sp.by]
			c, kind = keyCol(k), k.Kind
		}
		inner := fmt.Sprintf(`SELECT %s AS k, %s FROM %s WHERE %s GROUP BY %s`, c, aliased(sums), t, where, c)
		q := `SELECT t.k, ` + cols("t.v", len(sums)) + ` FROM (` + inner + `) t`
		switch kind {
		case KeyString:
			q = `SELECT d.s, ` + cols("t.v", len(sums)) + ` FROM (` + inner + `) t JOIN keys d ON d.id = t.k`
		case KeyNet:
			q = `SELECT d.s, ` + cols("t.v", len(sums)) + ` FROM (` + inner + `) t JOIN nets d ON d.id = t.k`
		}
		rows, err := e.read.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var label string
			if err := scanVals(rows, &label); err != nil {
				rows.Close()
				return err
			}
			out.addGroup(label, vals)
			for i, v := range vals {
				out.total[i] = satAdd(out.total[i], v)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	switch {
	case sp.step > 0:
		rows, err := e.read.QueryContext(ctx, fmt.Sprintf(`SELECT (bucket / %d) * %d AS b, %s FROM %s WHERE %s GROUP BY b`, sp.step, sp.step, strings.Join(sums, ", "), t, where), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var b int64
			if err := scanVals(rows, &b); err != nil {
				rows.Close()
				return err
			}
			out.series[b] = satAdd(out.series[b], vals[0])
			if sp.by == -1 {
				for i, v := range vals {
					out.total[i] = satAdd(out.total[i], v)
				}
			}
		}
		rows.Close()
		return rows.Err()
	case sp.by == -1:
		rows, err := e.read.QueryContext(ctx, `SELECT `+coalesced(sums)+` FROM `+t+` WHERE `+where, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			if err := scanVals(rows); err != nil {
				return err
			}
			for i, v := range vals {
				out.total[i] = satAdd(out.total[i], v)
			}
		}
		return rows.Err()
	}
	return nil
}

func aliased(sums []string) string {
	out := make([]string, len(sums))
	for i, s := range sums {
		out[i] = fmt.Sprintf("%s AS v%d", s, i)
	}
	return strings.Join(out, ", ")
}

func cols(prefix string, n int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return strings.Join(out, ", ")
}

func coalesced(sums []string) string {
	out := make([]string, len(sums))
	for i, s := range sums {
		out[i] = "COALESCE(" + s + ", 0)"
	}
	return strings.Join(out, ", ")
}

func (e *sqlEngine) stats(ctx context.Context) (Stats, error) {
	st := Stats{Bytes: map[Grain]int64{}, Rows: map[string]map[Grain]int64{}, FoldedRows: map[string]map[Grain]int64{},
		First: map[Grain]int64{}, Last: map[Grain]int64{}, Files: 1}
	byTable := map[string]Grain{}
	for _, f := range e.sch.fams {
		st.Rows[f.Name], st.FoldedRows[f.Name] = map[Grain]int64{}, map[Grain]int64{}
		for _, g := range e.grains {
			byTable[tableOf(f, g)] = g
		}
	}
	rows, err := e.read.QueryContext(ctx, `SELECT name, pgsize FROM dbstat WHERE aggregate = 1`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var name string
		var size int64
		if err := rows.Scan(&name, &size); err != nil {
			rows.Close()
			return st, err
		}
		if g, ok := byTable[name]; ok {
			st.Bytes[g] += size
		} else if name == "keys" || name == "nets" || strings.HasPrefix(name, "sqlite_autoindex_keys") || strings.HasPrefix(name, "sqlite_autoindex_nets") {
			st.DictBytes += size
		}
	}
	rows.Close()
	otherID, _, err := e.lookup(ctx, "keys", Other)
	if err != nil {
		return st, err
	}
	for _, f := range e.sch.fams {
		for _, g := range e.grains {
			t := tableOf(f, g)
			var n int64
			var lo, hi sql.NullInt64
			if err := e.read.QueryRowContext(ctx, `SELECT COUNT(*), MIN(bucket), MAX(bucket) FROM `+t).Scan(&n, &lo, &hi); err != nil {
				return st, err
			}
			st.Rows[f.Name][g] = n
			if lo.Valid {
				if v, ok := st.First[g]; !ok || lo.Int64 < v {
					st.First[g] = lo.Int64
				}
				if hi.Int64+g.Ms() > st.Last[g] {
					st.Last[g] = hi.Int64 + g.Ms()
				}
			}
			if f.rank >= 0 {
				if err := e.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t+` WHERE `+keyCol(f.Keys[f.rank])+` = ?`, otherID).Scan(&n); err != nil {
					return st, err
				}
				st.FoldedRows[f.Name][g] = n
			}
		}
	}
	for _, sfx := range []string{"", "-wal"} {
		if fi, err := os.Stat(e.path + sfx); err == nil {
			st.FileBytes += fi.Size()
		}
	}
	return st, nil
}

// sortedGrains lists tiers finest first.
func sortedGrains(m map[Grain]int64) []Grain {
	var out []Grain
	for g := range m {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
