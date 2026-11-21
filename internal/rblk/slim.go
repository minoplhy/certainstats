package rblk

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

const deleteBatch = 5000

// slimEngine keeps records in one SQLite file. Repeated strings live once in
// strs and rows hold their ids; a Dict column equal to its SameAs column
// stores 0; a Text column its template rebuilds is NULL; an ID that is a
// UUID is 16 bytes. As the head of the block engine it also remembers the
// dedupe values of rows already sealed (seen).
type slimEngine struct {
	si          *schemaInfo
	path        string
	write, read *sql.DB
	wmu         sync.Mutex
	cmu         sync.RWMutex
	ids         map[string]int64
	strs        map[int64]string
	head        bool
	// next is the next record id: ids are never reused, also after the
	// head forgets sealed rows.
	next int64
}

func slimDSN(path string) string {
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "synchronous(NORMAL)", "busy_timeout(5000)"} {
		q.Add("_pragma", p)
	}
	return "file:" + path + "?" + q.Encode()
}

func colName(c Column) string { return "c_" + c.Name }
func kidName(c Column) string { return "k_" + c.Name }
func sqlType(k Kind) string {
	switch k {
	case Text:
		return "TEXT"
	case ID:
		return "BLOB NOT NULL"
	}
	return "INTEGER NOT NULL"
}

func openSlim(path string, si *schemaInfo, head bool) (*slimEngine, error) {
	w, err := sql.Open("sqlite", slimDSN(path))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	cols := []string{"id INTEGER PRIMARY KEY", "owner INTEGER NOT NULL", "t INTEGER NOT NULL", "nchild INTEGER NOT NULL"}
	for _, c := range si.Columns {
		cols = append(cols, colName(c)+" "+sqlType(c.Kind))
	}
	kids := []string{"rec INTEGER NOT NULL", "seq INTEGER NOT NULL"}
	for _, c := range si.Children {
		kids = append(kids, kidName(c)+" "+sqlType(c.Kind))
	}
	// Checked before the tables, whose indexes name the schema's columns.
	if _, err := w.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL)`); err != nil {
		w.Close()
		return nil, fmt.Errorf("rblk schema: %w", err)
	}
	// The first open records the schema's shape; later opens must match it.
	// A store from before shapes were recorded trusts the schema it is given.
	var shape int64
	switch err := w.QueryRow(`SELECT value FROM meta WHERE key = 'shape'`).Scan(&shape); {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := w.Exec(`INSERT INTO meta (key, value) VALUES ('shape', ?)`, si.shape()); err != nil {
			w.Close()
			return nil, err
		}
	case err != nil:
		w.Close()
		return nil, err
	case shape != si.shape():
		w.Close()
		return nil, ErrShapeChanged
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS strs (id INTEGER PRIMARY KEY, s TEXT NOT NULL UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS rec (` + strings.Join(cols, ", ") + `)`,
		// nchild rides along so time-bucket counts (timelines) read the index only.
		`CREATE INDEX IF NOT EXISTS rec_t ON rec(owner, t, nchild)`,
		`CREATE TABLE IF NOT EXISTS child (` + strings.Join(kids, ", ") + `, PRIMARY KEY (rec, seq)) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL)`,
	}
	if si.dedupe >= 0 {
		stmts = append(stmts, `CREATE UNIQUE INDEX IF NOT EXISTS rec_dedupe ON rec(owner, `+colName(si.Columns[si.dedupe])+`)`)
	}
	for _, c := range si.Columns {
		if c.Index {
			stmts = append(stmts, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS rec_%s ON rec(owner, %s, t)`, c.Name, colName(c)))
		}
	}
	if si.activeCh >= 0 {
		stmts = append(stmts, `CREATE INDEX IF NOT EXISTS child_active ON child(`+kidName(si.Children[si.activeCh])+`)`)
	}
	if head {
		stmts = append(stmts, `CREATE TABLE IF NOT EXISTS seen (owner INTEGER NOT NULL, v BLOB NOT NULL, t INTEGER NOT NULL, PRIMARY KEY (owner, v)) WITHOUT ROWID`,
			`CREATE INDEX IF NOT EXISTS seen_t ON seen(t)`)
	}
	for _, s := range stmts {
		if _, err := w.Exec(s); err != nil {
			w.Close()
			return nil, fmt.Errorf("rblk schema: %w", err)
		}
	}
	r, err := sql.Open("sqlite", slimDSN(path))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	e := &slimEngine{si: si, path: path, write: w, read: r, head: head}
	if err := e.loadStrs(); err != nil {
		e.close()
		return nil, err
	}
	var maxID, saved int64
	w.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM rec`).Scan(&maxID)
	w.QueryRow(`SELECT COALESCE((SELECT value FROM meta WHERE key = 'next_id'), 0)`).Scan(&saved)
	e.next = max(maxID+1, saved, 1)
	return e, nil
}

func (e *slimEngine) close() error {
	e.read.Close()
	return e.write.Close()
}

func (e *slimEngine) loadStrs() error {
	e.cmu.Lock()
	defer e.cmu.Unlock()
	e.ids, e.strs = map[string]int64{}, map[int64]string{}
	rows, err := e.write.Query(`SELECT id, s FROM strs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var s string
		if err := rows.Scan(&id, &s); err != nil {
			return err
		}
		e.ids[s], e.strs[id] = id, s
	}
	return rows.Err()
}

func (e *slimEngine) lookup(s string) (int64, bool) {
	e.cmu.RLock()
	defer e.cmu.RUnlock()
	id, ok := e.ids[s]
	return id, ok
}

func (e *slimEngine) str(id int64) string {
	e.cmu.RLock()
	defer e.cmu.RUnlock()
	return e.strs[id]
}

func (e *slimEngine) intern(ctx context.Context, tx *sql.Tx, s string) (int64, error) {
	if id, ok := e.lookup(s); ok {
		return id, nil
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO strs (s) VALUES (?) ON CONFLICT (s) DO UPDATE SET s = excluded.s RETURNING id`, s).Scan(&id); err != nil {
		return 0, err
	}
	e.cmu.Lock()
	e.ids[s], e.strs[id] = id, s
	e.cmu.Unlock()
	return id, nil
}

// encodeID stores an ID so that only a UUID is ever exactly 16 bytes:
//   - a canonical lower-case UUID: its 16 bytes;
//   - text of 15 bytes: 0x02, 0x00, then the text (17 bytes);
//   - any other text: 0x00, then the text.
//
// Without the 0x02 form, 15 bytes of text plus its 0x00 would read back as a UUID.
func encodeID(s string) []byte {
	if len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-' {
		h := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:]
		if b, err := hex.DecodeString(h); err == nil && hex.EncodeToString(b) == h {
			return b
		}
	}
	if len(s) == 15 {
		return append([]byte{2, 0}, s...)
	}
	return append([]byte{0}, s...)
}

// validID reports whether b is one of the three stored forms of Section 8.4.
func validID(b []byte) bool {
	switch {
	case len(b) == 16:
		return true
	case len(b) == 0:
		return false
	case b[0] == 0:
		return len(b) != 16
	case b[0] == 2:
		return len(b) == 17 && b[1] == 0
	}
	return false
}

func decodeID(b []byte) string {
	switch {
	case len(b) == 16:
		h := hex.EncodeToString(b)
		return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	case len(b) == 0:
		return ""
	case b[0] == 2 && len(b) >= 2:
		return string(b[2:])
	}
	return string(b[1:])
}

// dictValue is what a Dict column stores: 0 when it equals its SameAs column.
func (e *slimEngine) dictValue(ctx context.Context, tx *sql.Tx, v, same string, hasSame bool) (int64, error) {
	if hasSame && v == same {
		return 0, nil
	}
	return e.intern(ctx, tx, v)
}

func (e *slimEngine) insert(ctx context.Context, owner string, recs []Record) ([]int64, error) {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	next := e.next
	ids, err := e.insertLocked(ctx, owner, recs)
	if err != nil {
		e.next = next
		e.loadStrs() // strings interned by the rolled-back transaction are gone
	}
	return ids, err
}

// insertLocked writes new rows with the next ids, skipping duplicates.
func (e *slimEngine) insertLocked(ctx context.Context, owner string, recs []Record) ([]int64, error) {
	si := e.si
	tx, err := e.write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	oid, err := e.intern(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	cols := []string{"owner", "t", "nchild", "id"}
	for _, c := range si.Columns {
		cols = append(cols, colName(c))
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO rec (`+strings.Join(cols, ", ")+`) VALUES (`+strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")+`) ON CONFLICT DO NOTHING RETURNING id`)
	var dup *sql.Stmt
	if si.dedupe >= 0 && err == nil {
		dup, err = tx.PrepareContext(ctx, `SELECT 1 FROM rec WHERE owner = ? AND `+colName(si.Columns[si.dedupe])+` = ?`)
		if dup != nil {
			defer dup.Close()
		}
	}
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	kcols := []string{"rec", "seq"}
	for _, c := range si.Children {
		kcols = append(kcols, kidName(c))
	}
	kstmt, err := tx.PrepareContext(ctx, `INSERT INTO child (`+strings.Join(kcols, ", ")+`) VALUES (`+strings.TrimSuffix(strings.Repeat("?, ", len(kcols)), ", ")+`)`)
	if err != nil {
		return nil, err
	}
	defer kstmt.Close()
	var seen *sql.Stmt
	if e.head && si.dedupe >= 0 {
		if seen, err = tx.PrepareContext(ctx, `SELECT 1 FROM seen WHERE owner = ? AND v = ?`); err != nil {
			return nil, err
		}
		defer seen.Close()
	}
	out := make([]int64, len(recs))
	args := make([]any, 0, len(cols))
	for i := range recs {
		r := &recs[i]
		for _, check := range []*sql.Stmt{seen, dup} {
			if check == nil {
				continue
			}
			var one int
			err := check.QueryRowContext(ctx, oid, encodeID(r.S[si.dedupe])).Scan(&one)
			if err == nil {
				goto skip // stored, or sealed, already
			}
			if err != sql.ErrNoRows {
				return nil, err
			}
		}
		args = append(args[:0], oid, r.Time, len(r.Children), e.next)
		e.next++
		for j, c := range si.Columns {
			switch c.Kind {
			case Dict:
				same := si.same[j]
				v, err := e.dictValue(ctx, tx, r.S[j], safeS(r.S, same), same >= 0)
				if err != nil {
					return nil, err
				}
				args = append(args, v)
			case Int, Bool:
				args = append(args, r.N[j])
			case Text:
				if tpl := si.Templates[c.Name]; tpl != nil && tpl(r) == r.S[j] {
					args = append(args, nil)
				} else {
					args = append(args, r.S[j])
				}
			case ID:
				args = append(args, encodeID(r.S[j]))
			}
		}
		{
			var id int64
			if err := stmt.QueryRowContext(ctx, args...).Scan(&id); err != nil {
				return nil, err
			}
			out[i] = id
			for seq, ch := range r.Children {
				kargs := []any{id, seq}
				for j, c := range si.Children {
					switch c.Kind {
					case Dict:
						same := si.csame[j]
						v, err := e.dictValue(ctx, tx, ch.S[j], safeS(r.S, same), same >= 0)
						if err != nil {
							return nil, err
						}
						kargs = append(kargs, v)
					case Int, Bool:
						kargs = append(kargs, ch.N[j])
					case Text:
						kargs = append(kargs, ch.S[j])
					case ID:
						kargs = append(kargs, encodeID(ch.S[j]))
					}
				}
				if _, err := kstmt.ExecContext(ctx, kargs...); err != nil {
					return nil, err
				}
			}
		}
	skip:
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('next_id', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, e.next); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func safeS(s []string, i int) string {
	if i < 0 {
		return ""
	}
	return s[i]
}

// expr is a column as SQL, with SameAs resolved.
func (e *slimEngine) expr(j int) string {
	c := e.si.Columns[j]
	if s := e.si.same[j]; s >= 0 {
		return fmt.Sprintf("(CASE %s WHEN 0 THEN %s ELSE %s END)", colName(c), colName(e.si.Columns[s]), colName(c))
	}
	return colName(c)
}

// where compiles a query; ok is false when nothing can match.
func (e *slimEngine) where(q Query, withTime bool) (string, []any, bool) {
	oid, ok := e.lookup(q.Owner)
	if !ok {
		return "", nil, false
	}
	conds := []string{"owner = ?"}
	args := []any{oid}
	if withTime && q.End != 0 {
		conds = append(conds, "t >= ?", "t < ?")
		args = append(args, q.Start, q.End)
	}
	for _, f := range q.Filters {
		j := e.si.col(f.Col)
		var ids []any
		for _, v := range f.Values {
			if id, ok := e.lookup(v); ok {
				ids = append(ids, id)
			}
		}
		switch {
		case len(ids) == 0 && f.Not:
			continue
		case len(ids) == 0:
			return "", nil, false
		}
		op := " IN ("
		if f.Not {
			op = " NOT IN ("
		}
		conds = append(conds, e.expr(j)+op+strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")+")")
		args = append(args, ids...)
	}
	if q.Contains != "" {
		// Resolve the search once against the string table in memory: the
		// same strings LIKE '%q%' would find, as one id list for every column.
		lq := strings.ToLower(q.Contains)
		var hit []int64
		e.cmu.RLock()
		for id, str := range e.strs {
			if containsFold(str, lq) {
				hit = append(hit, id)
			}
		}
		e.cmu.RUnlock()
		if len(hit) == 0 {
			return "", nil, false
		}
		list, _ := json.Marshal(hit)
		var parts []string
		for _, c := range q.In {
			parts = append(parts, e.expr(e.si.col(c))+` IN (SELECT value FROM json_each(?))`)
			args = append(args, string(list))
		}
		conds = append(conds, "("+strings.Join(parts, " OR ")+")")
	}
	return strings.Join(conds, " AND "), args, true
}

func (e *slimEngine) selectCols() string {
	cols := []string{"rec.id", "rec.t", "rec.nchild"}
	for _, c := range e.si.Columns {
		cols = append(cols, "rec."+colName(c))
	}
	return strings.Join(cols, ", ")
}

// scanRec decodes one row of selectCols.
func (e *slimEngine) scanRec(rows interface{ Scan(...any) error }) (Record, int64, error) {
	si := e.si
	n := len(si.Columns)
	raw := make([]any, 3+n)
	var id, t, nchild int64
	raw[0], raw[1], raw[2] = &id, &t, &nchild
	ints := make([]int64, n)
	texts := make([]sql.NullString, n)
	blobs := make([][]byte, n)
	for j, c := range si.Columns {
		switch c.Kind {
		case Text:
			raw[3+j] = &texts[j]
		case ID:
			raw[3+j] = &blobs[j]
		default:
			raw[3+j] = &ints[j]
		}
	}
	if err := rows.Scan(raw...); err != nil {
		return Record{}, 0, err
	}
	r := Record{ID: id, Time: t, S: make([]string, n), N: make([]int64, n)}
	for j, c := range si.Columns {
		switch c.Kind {
		case Dict:
			if si.same[j] < 0 || ints[j] != 0 {
				r.S[j] = e.str(ints[j])
			}
		case Int, Bool:
			r.N[j] = ints[j]
		case ID:
			r.S[j] = decodeID(blobs[j])
		}
	}
	for j := range si.Columns {
		if s := si.same[j]; s >= 0 && ints[j] == 0 {
			r.S[j] = r.S[s]
		}
	}
	for j, c := range si.Columns {
		if c.Kind == Text {
			if texts[j].Valid {
				r.S[j] = texts[j].String
			} else if tpl := si.Templates[c.Name]; tpl != nil {
				r.S[j] = tpl(&r)
			}
		}
	}
	return r, nchild, nil
}

// children fills the child rows of recs (by ID), keeping those for which keep is true.
func (e *slimEngine) children(ctx context.Context, recs []Record, keep func(*Child) bool) error {
	if len(recs) == 0 || len(e.si.Children) == 0 {
		return nil
	}
	pos := map[int64]int{}
	for i := range recs {
		pos[recs[i].ID] = i
	}
	si := e.si
	cols := []string{"rec"}
	for _, c := range si.Children {
		cols = append(cols, kidName(c))
	}
	for lo := 0; lo < len(recs); lo += 500 {
		hi := min(lo+500, len(recs))
		args := make([]any, 0, hi-lo)
		for _, r := range recs[lo:hi] {
			args = append(args, r.ID)
		}
		rows, err := e.read.QueryContext(ctx, `SELECT `+strings.Join(cols, ", ")+` FROM child WHERE rec IN (`+strings.TrimSuffix(strings.Repeat("?, ", len(args)), ", ")+`) ORDER BY rec, seq`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var rec int64
			raw := []any{&rec}
			ints := make([]int64, len(si.Children))
			texts := make([]sql.NullString, len(si.Children))
			blobs := make([][]byte, len(si.Children))
			for j, c := range si.Children {
				switch c.Kind {
				case Text:
					raw = append(raw, &texts[j])
				case ID:
					raw = append(raw, &blobs[j])
				default:
					raw = append(raw, &ints[j])
				}
			}
			if err := rows.Scan(raw...); err != nil {
				rows.Close()
				return err
			}
			parent := &recs[pos[rec]]
			ch := Child{S: make([]string, len(si.Children)), N: make([]int64, len(si.Children))}
			for j, c := range si.Children {
				switch c.Kind {
				case Dict:
					if s := si.csame[j]; s >= 0 && ints[j] == 0 {
						ch.S[j] = parent.S[s]
					} else {
						ch.S[j] = e.str(ints[j])
					}
				case Int, Bool:
					ch.N[j] = ints[j]
				case Text:
					ch.S[j] = texts[j].String
				case ID:
					ch.S[j] = decodeID(blobs[j])
				}
			}
			if keep == nil || keep(&ch) {
				parent.Children = append(parent.Children, ch)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (e *slimEngine) get(ctx context.Context, owner string, id int64) (*Record, error) {
	oid, ok := e.lookup(owner)
	if !ok {
		return nil, ErrNotFound
	}
	r, _, err := e.scanRec(e.read.QueryRowContext(ctx, `SELECT `+e.selectCols()+` FROM rec WHERE id = ? AND owner = ?`, id, oid))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	list := []Record{r}
	if err := e.children(ctx, list, nil); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// from and orderBy give a find query's FROM and ORDER BY. A Dict sort joins
// strs and orders by its text; for an indexed column that is not SameAs and
// an unfiltered query, strs leads (its unique index on s is already in order) and each string
// probes the column's index, so a first page never sorts every row.
func (e *slimEngine) from(q Query) string {
	if q.Sort == "" {
		return "rec"
	}
	j := e.si.col(q.Sort)
	c := e.si.Columns[j]
	if c.Kind != Dict {
		return "rec"
	}
	// Only unfiltered: with filters SQLite may probe the time index per
	// string, and walking every string costs more than sorting the matches.
	if c.Index && e.si.same[j] < 0 && len(q.Filters) == 0 && q.Contains == "" {
		return "strs AS so CROSS JOIN rec INDEXED BY rec_" + c.Name + " ON rec." + colName(c) + " = so.id"
	}
	return "rec LEFT JOIN strs AS so ON so.id = " + e.expr(j)
}

func (e *slimEngine) orderBy(q Query) string {
	dir := " ASC"
	if q.Desc {
		dir = " DESC"
	}
	o := "rec.t"
	if q.Sort != "" {
		j := e.si.col(q.Sort)
		o = "rec." + colName(e.si.Columns[j])
		if e.si.Columns[j].Kind == Dict {
			o = "so.s"
		}
	}
	return o + dir + ", rec.id" + dir
}

func (e *slimEngine) find(ctx context.Context, q Query) ([]Record, int64, error) {
	where, args, ok := e.where(q, true)
	if !ok {
		return []Record{}, 0, nil
	}
	var total int64
	if err := e.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM rec WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = -1
	}
	rows, err := e.read.QueryContext(ctx, `SELECT `+e.selectCols()+` FROM `+e.from(q)+` WHERE `+where+` ORDER BY `+e.orderBy(q)+` LIMIT ? OFFSET ?`, append(args, limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	out := []Record{}
	for rows.Next() {
		r, _, err := e.scanRec(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if q.Children {
		if err := e.children(ctx, out, nil); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// headKey is one row's place in a sort order, without the row.
type headKey struct {
	id, t, n int64
	s        string
}

// findKeys returns the first k rows' ids and sort keys (k 0: all) and the
// match count, reading no more of each row than the order needs; with the
// time index that is the index alone.
func (e *slimEngine) findKeys(ctx context.Context, q Query, k int) ([]headKey, int64, error) {
	where, args, ok := e.where(q, true)
	if !ok {
		return nil, 0, nil
	}
	var total int64
	if err := e.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM rec WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	key, dict := "0", false
	if q.Sort != "" {
		j := e.si.col(q.Sort)
		key = "rec." + colName(e.si.Columns[j])
		if e.si.Columns[j].Kind == Dict {
			key, dict = e.expr(j), true
		}
	}
	limit := k
	if limit <= 0 {
		limit = -1
	}
	rows, err := e.read.QueryContext(ctx, `SELECT rec.id, rec.t, `+key+` FROM `+e.from(q)+` WHERE `+where+` ORDER BY `+e.orderBy(q)+` LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []headKey
	for rows.Next() {
		var h headKey
		if err := rows.Scan(&h.id, &h.t, &h.n); err != nil {
			return nil, 0, err
		}
		if dict {
			h.s = e.str(h.n)
		}
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// getMany returns the owner's rows with these ids, in the order given.
func (e *slimEngine) getMany(ctx context.Context, owner string, ids []int64, children bool) ([]Record, error) {
	oid, ok := e.lookup(owner)
	if !ok || len(ids) == 0 {
		return nil, nil
	}
	list, _ := json.Marshal(ids)
	rows, err := e.read.QueryContext(ctx, `SELECT `+e.selectCols()+` FROM rec WHERE owner = ? AND id IN (SELECT value FROM json_each(?))`, oid, string(list))
	if err != nil {
		return nil, err
	}
	byID := map[int64]Record{}
	for rows.Next() {
		r, _, err := e.scanRec(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		byID[r.ID] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	if children {
		if err := e.children(ctx, out, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (e *slimEngine) aggregate(ctx context.Context, q Query, a Agg) ([]Group, error) {
	where, args, ok := e.where(q, true)
	if !ok {
		return []Group{}, nil
	}
	si := e.si
	var sel, group, joins []string
	key := "''"
	if a.By != "" {
		// Unary + keeps SQLite from walking the column's index just to
		// skip the GROUP BY sort: that reads every row of the owner and
		// ignores the index the WHERE clause would use (an IP page grouping
		// one IP's rows by scenario went from 3 ms to 0.5 s).
		key = "+" + e.expr(si.col(a.By))
		group = append(group, key)
	}
	bucket := "0"
	if a.Width > 0 {
		bucket = fmt.Sprintf("((t + %d) / %d) * %d - %d", a.Offset, a.Width, a.Width, a.Offset)
		group = append(group, bucket)
	}
	sel = append(sel, key+" AS k", bucket+" AS b", "COUNT(*) AS c")
	for _, s := range a.Sum {
		if s == "#children" {
			sel = append(sel, "COALESCE(SUM(nchild), 0)")
		} else {
			sel = append(sel, "COALESCE(SUM("+colName(si.Columns[si.col(s)])+"), 0)")
		}
	}
	if a.Distinct != "" {
		empty, hasEmpty := e.lookup("")
		if !hasEmpty {
			empty = -1
		}
		sel = append(sel, fmt.Sprintf("COUNT(DISTINCT NULLIF(%s, %d))", e.expr(si.col(a.Distinct)), empty))
	} else {
		sel = append(sel, "0")
	}
	// Max compares text, so each Max column joins strs once.
	for i, m := range a.Max {
		alias := fmt.Sprintf("m%d", i)
		joins = append(joins, fmt.Sprintf(" LEFT JOIN strs AS %s ON %s.id = %s", alias, alias, e.expr(si.col(m))))
		sel = append(sel, "MAX("+alias+".s)")
	}
	if a.Times {
		sel = append(sel, "MIN(t)", "MAX(t)")
	} else {
		sel = append(sel, "NULL", "NULL")
	}
	query := `SELECT ` + strings.Join(sel, ", ") + ` FROM rec` + strings.Join(joins, "") + ` WHERE ` + where
	if len(group) > 0 {
		query += ` GROUP BY ` + strings.Join(group, ", ")
	}
	if a.Top > 0 {
		// Rank groups in SQL; only the groups kept look up their key text.
		query = `SELECT g.*, SUM(g.c) OVER () FROM (` + query + `) AS g LEFT JOIN strs AS ks ON ks.id = g.k ORDER BY g.c DESC, COALESCE(ks.s, ''), g.b LIMIT ` + strconv.Itoa(a.Top)
	} else {
		query = `SELECT *, 0 FROM (` + query + `)`
	}
	rows, err := e.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var keyRaw any
		g := Group{Sums: make([]int64, len(a.Sum)), Max: make([]string, len(a.Max))}
		dest := []any{&keyRaw, &g.Bucket, &g.Count}
		for i := range g.Sums {
			dest = append(dest, &g.Sums[i])
		}
		dest = append(dest, &g.Distinct)
		maxes := make([]sql.NullString, len(a.Max))
		for i := range maxes {
			dest = append(dest, &maxes[i])
		}
		var mn, mx sql.NullInt64
		dest = append(dest, &mn, &mx, &g.Total)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if g.Count == 0 {
			continue
		}
		if a.By != "" {
			if k, ok := keyRaw.(int64); ok {
				g.Key = e.str(k)
			}
		}
		for i := range maxes {
			g.Max[i] = maxes[i].String
		}
		g.MinTime, g.MaxTime = mn.Int64, mx.Int64
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if a.Top <= 0 {
		sortGroups(out)
	}
	return out, nil
}

// distinctValues calls add with each group's distinct non-empty values of
// a.Distinct, for merging with other parts of a store.
func (e *slimEngine) distinctValues(ctx context.Context, q Query, a Agg, add func(groupKey, string)) error {
	where, args, ok := e.where(q, true)
	if !ok {
		return nil
	}
	si := e.si
	key := "0"
	if a.By != "" {
		key = e.expr(si.col(a.By))
	}
	bucket := "0"
	if a.Width > 0 {
		bucket = fmt.Sprintf("((t + %d) / %d) * %d - %d", a.Offset, a.Width, a.Width, a.Offset)
	}
	rows, err := e.read.QueryContext(ctx, `SELECT DISTINCT `+key+`, `+bucket+`, `+e.expr(si.col(a.Distinct))+` FROM rec WHERE `+where, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k, b, v int64
		if err := rows.Scan(&k, &b, &v); err != nil {
			return err
		}
		gk := groupKey{bucket: b}
		if a.By != "" {
			gk.key = e.str(k)
		}
		add(gk, e.str(v))
	}
	return rows.Err()
}

func (e *slimEngine) active(ctx context.Context, q Query, now int64) ([]Record, error) {
	if e.si.activeCh < 0 {
		return []Record{}, nil
	}
	where, args, ok := e.where(q, false)
	if !ok {
		return []Record{}, nil
	}
	act := kidName(e.si.Children[e.si.activeCh])
	rows, err := e.read.QueryContext(ctx, `SELECT `+e.selectCols()+` FROM rec WHERE `+where+` AND id IN (SELECT rec FROM child WHERE `+act+` > ?) ORDER BY id`, append(args, now)...)
	if err != nil {
		return nil, err
	}
	out := []Record{}
	for rows.Next() {
		r, _, err := e.scanRec(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ac := e.si.activeCh
	return out, e.children(ctx, out, func(c *Child) bool { return c.N[ac] > now })
}

func (e *slimEngine) maintain(ctx context.Context, now int64) error { return nil }

func (e *slimEngine) deleteBatches(ctx context.Context, sel string, args ...any) error {
	for {
		tx, err := e.write.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		ids := []any{}
		rows, err := tx.QueryContext(ctx, sel+` LIMIT ?`, append(args, deleteBatch)...)
		if err != nil {
			tx.Rollback()
			return err
		}
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		if len(ids) == 0 {
			tx.Rollback()
			return nil
		}
		in := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
		if _, err := tx.ExecContext(ctx, `DELETE FROM child WHERE rec IN (`+in+`)`, ids...); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM rec WHERE id IN (`+in+`)`, ids...); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if len(ids) < deleteBatch || ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (e *slimEngine) prune(ctx context.Context, before int64, owners []string) error {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	if owners != nil {
		keep := map[int64]bool{}
		for _, o := range owners {
			if id, ok := e.lookup(o); ok {
				keep[id] = true
			}
		}
		rows, err := e.read.QueryContext(ctx, `SELECT DISTINCT owner FROM rec`)
		if err != nil {
			return err
		}
		var drop []int64
		for rows.Next() {
			var o int64
			rows.Scan(&o)
			if !keep[o] {
				drop = append(drop, o)
			}
		}
		rows.Close()
		for _, o := range drop {
			if err := e.deleteBatches(ctx, `SELECT id FROM rec WHERE owner = ?`, o); err != nil {
				return err
			}
			if e.head {
				if _, err := e.write.ExecContext(ctx, `DELETE FROM seen WHERE owner = ?`, o); err != nil {
					return err
				}
			}
		}
	}
	if before > 0 {
		return e.deleteBatches(ctx, `SELECT id FROM rec WHERE t < ?`, before)
	}
	return nil
}

func (e *slimEngine) stats(ctx context.Context) (Stats, error) {
	var st Stats
	for _, suffix := range []string{"", "-wal"} {
		if n, f := fileSize(e.path + suffix); f {
			st.FileBytes += n
			st.Files++
		}
	}
	err := e.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM rec`).Scan(&st.HeadRows)
	st.Rows = st.HeadRows
	return st, err
}

func (e *slimEngine) oldest(ctx context.Context, owner string) (int64, bool, error) {
	oid, ok := e.lookup(owner)
	if !ok {
		return 0, false, nil
	}
	var t sql.NullInt64
	if err := e.read.QueryRowContext(ctx, `SELECT MIN(t) FROM rec WHERE owner = ?`, oid).Scan(&t); err != nil {
		return 0, false, err
	}
	return t.Int64, t.Valid, nil
}

func fileSize(path string) (int64, bool) {
	n, files := dirBytes(path)
	return n, files > 0
}

// ---- head helpers for the block engine ----

// dump returns every row with Time in [start, end), with children, per owner.
func (e *slimEngine) dump(ctx context.Context, start, end int64) (map[string][]Record, error) {
	rows, err := e.read.QueryContext(ctx, `SELECT owner, `+e.selectCols()+` FROM rec WHERE t >= ? AND t < ? ORDER BY owner, t, id`, start, end)
	if err != nil {
		return nil, err
	}
	byOwner := map[string][]Record{}
	for rows.Next() {
		var oid int64
		var r Record
		err := e.scanWithOwner(rows, &oid, &r)
		if err != nil {
			rows.Close()
			return nil, err
		}
		o := e.str(oid)
		byOwner[o] = append(byOwner[o], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for o, list := range byOwner {
		if err := e.children(ctx, list, nil); err != nil {
			return nil, err
		}
		byOwner[o] = list
	}
	return byOwner, nil
}

func (e *slimEngine) scanWithOwner(rows *sql.Rows, oid *int64, r *Record) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return err
	}
	*oid = vals[0].(int64)
	rec, _, err := e.scanRec(rowOf(vals[1:]))
	*r = rec
	return err
}

// rowOf replays scanned values into Scan targets.
type rowOf []any

func (v rowOf) Scan(dest ...any) error {
	for i, d := range dest {
		switch p := d.(type) {
		case *int64:
			*p, _ = v[i].(int64)
		case *sql.NullString:
			if s, ok := v[i].(string); ok {
				*p = sql.NullString{String: s, Valid: true}
			} else {
				*p = sql.NullString{}
			}
		case *[]byte:
			b, _ := v[i].([]byte)
			*p = append([]byte(nil), b...)
		default:
			return fmt.Errorf("rblk: cannot scan into %T", d)
		}
	}
	return nil
}

// forget deletes the rows in [start, end) and remembers their dedupe values.
func (e *slimEngine) forget(ctx context.Context, start, end int64) error {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	tx, err := e.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if e.si.dedupe >= 0 {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO seen (owner, v, t) SELECT owner, `+colName(e.si.Columns[e.si.dedupe])+`, t FROM rec WHERE t >= ? AND t < ?`, start, end); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM child WHERE rec IN (SELECT id FROM rec WHERE t >= ? AND t < ?)`, start, end); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM rec WHERE t >= ? AND t < ?`, start, end); err != nil {
		return err
	}
	return tx.Commit()
}

// days lists the day starts that hold rows older than before.
func (e *slimEngine) days(ctx context.Context, before int64) ([]int64, error) {
	rows, err := e.read.QueryContext(ctx, `SELECT DISTINCT (t / `+strconv.FormatInt(dayMs, 10)+`) * `+strconv.FormatInt(dayMs, 10)+` FROM rec WHERE t < ? ORDER BY 1`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var d int64
		rows.Scan(&d)
		out = append(out, d)
	}
	return out, rows.Err()
}
