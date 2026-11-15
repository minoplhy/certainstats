package crowdsec

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"certainstats/internal/crowdsec/wire"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// DefaultDatabase is where a host install of CrowdSec keeps its SQLite database.
const DefaultDatabase = "/var/lib/crowdsec/data/crowdsec.db"

// healthConfig is the host's decision about reading CrowdSec's database. It is
// off unless CROWDSEC_HEALTH=true; the panel cannot switch it on.
type healthConfig struct {
	on    bool
	dsn   string
	parts []string // in wire.Parts order
	err   error    // a setting that could not be understood
}

func (c healthConfig) reads(part string) bool {
	for _, p := range c.parts {
		if p == part {
			return true
		}
	}
	return false
}

func healthFromEnv(getenv func(string) string) healthConfig {
	var c healthConfig
	if !strings.EqualFold(strings.TrimSpace(getenv("CROWDSEC_HEALTH")), "true") {
		return c
	}
	c.on = true
	c.dsn = strings.TrimSpace(getenv("CROWDSEC_DB"))
	if c.dsn == "" {
		c.dsn = DefaultDatabase
	}
	raw := strings.TrimSpace(getenv("CROWDSEC_HEALTH_READ"))
	if raw == "" {
		c.parts = append(c.parts, wire.Parts...)
		return c
	}
	want := map[string]bool{}
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' }) {
		want[strings.ToLower(f)] = true
	}
	for _, p := range wire.Parts {
		if want[p] {
			c.parts = append(c.parts, p)
			delete(want, p)
		}
	}
	for unknown := range want {
		c.err = fmt.Errorf("CROWDSEC_HEALTH_READ: unknown part %q; use %s", unknown, strings.Join(wire.Parts, ", "))
	}
	return c
}

// healthDB is a read-only handle on CrowdSec's database. Nothing here can
// write: SQLite is opened with mode=ro and query_only, and every PostgreSQL or
// MySQL query runs in a read-only transaction. It never creates a file, changes
// permissions or runs a migration, which CrowdSec's own client does on open.
type healthDB struct {
	db      *sql.DB
	dialect string // "sqlite", "postgres" or "mysql"
	q       queries
}

func dialectOf(dsn string) string {
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return "postgres"
	case strings.HasPrefix(dsn, "mysql://"):
		return "mysql"
	}
	return "sqlite"
}

func openHealthDB(dsn string) (*healthDB, error) {
	h := &healthDB{dialect: dialectOf(dsn), q: sqliteQueries}
	var err error
	switch h.dialect {
	case "postgres":
		h.q = postgresQueries
		var cfg *pgx.ConnConfig
		if cfg, err = pgx.ParseConfig(dsn); err != nil {
			// The parser's message can repeat the DSN, password included.
			return nil, fmt.Errorf("CROWDSEC_DB is not a valid postgres:// address")
		}
		cfg.RuntimeParams["default_transaction_read_only"] = "on"
		h.db = stdlib.OpenDB(*cfg)
	case "mysql":
		h.q = mysqlQueries
		var cfg *mysql.Config
		if cfg, err = mysqlConfig(dsn); err != nil {
			return nil, err
		}
		conn, cerr := mysql.NewConnector(cfg)
		if cerr != nil {
			return nil, fmt.Errorf("CROWDSEC_DB: %w", cerr)
		}
		h.db = sql.OpenDB(conn)
	default:
		// mode=ro fails on a missing file instead of creating it.
		q := url.Values{}
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(1)")
		q.Add("_pragma", "busy_timeout(5000)")
		h.db, err = sql.Open("sqlite", "file:"+(&url.URL{Path: dsn}).EscapedPath()+"?"+q.Encode())
		if err != nil {
			return nil, err
		}
	}
	h.db.SetMaxOpenConns(1)
	h.db.SetConnMaxIdleTime(5 * time.Minute)
	return h, nil
}

// mysqlConfig turns mysql://user:pass@host:port/db?tls=… into the driver's settings.
func mysqlConfig(dsn string) (*mysql.Config, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" || strings.Trim(u.Path, "/") == "" {
		return nil, fmt.Errorf("CROWDSEC_DB must look like mysql://user:password@host:3306/database")
	}
	cfg := mysql.NewConfig()
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password() // the second value only says whether one was given
	cfg.Net = "tcp"
	cfg.Addr = u.Host
	if u.Port() == "" {
		cfg.Addr = net.JoinHostPort(u.Hostname(), "3306")
	}
	cfg.DBName = strings.Trim(u.Path, "/")
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	if tls := u.Query().Get("tls"); tls != "" {
		cfg.TLSConfig = tls
	}
	return cfg, nil
}

func (h *healthDB) close() { h.db.Close() }

// timeArg is a point in time in the form the dialect compares stored times with.
// SQLite keeps times as text starting "2006-01-02 15:04:05", in UTC.
func (h *healthDB) timeArg(t time.Time) any {
	if h.dialect == "postgres" {
		return t.UTC()
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

// query runs one read. PostgreSQL and MySQL get a read-only transaction, so even
// a database user that could write cannot do so through the collector.
func (h *healthDB) query(ctx context.Context, each func(*sql.Rows) error, query string, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var rows *sql.Rows
	var err error
	if h.dialect == "sqlite" {
		rows, err = h.db.QueryContext(ctx, query, args...)
	} else {
		var tx *sql.Tx
		if tx, err = h.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true}); err != nil {
			return err
		}
		defer tx.Rollback()
		rows, err = tx.QueryContext(ctx, query, args...)
	}
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := each(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

var timeLayouts = []string{
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
}

// dbTimeMs reads a time column whatever the driver hands back: a time, or the
// text SQLite and MySQL keep. Zero when empty or unreadable.
func dbTimeMs(v any) int64 {
	var s string
	switch x := v.(type) {
	case time.Time:
		if x.IsZero() || x.Year() < 2000 {
			return 0
		}
		return x.UnixMilli()
	case string:
		s = x
	case []byte:
		s = string(x)
	default:
		return 0
	}
	if t, ok := wire.ParseTime(s); ok {
		return t.UnixMilli()
	}
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil && t.Year() >= 2000 {
			return t.UnixMilli()
		}
	}
	return 0
}

func dbBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case []byte:
		return dbBool(string(x))
	case string:
		return x == "1" || strings.EqualFold(x, "t") || strings.EqualFold(x, "true")
	}
	return false
}

func dbText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	}
	return ""
}
