package sqlite

import (
	"certainstats/internal/store"
	"database/sql"
	"sync"
)

// Store implements store.AgentStore, store.SessionStore,
// store.UserStore, and store.DashboardStore against SQLite.
// The compiler enforces this via the var _ checks below.
type Store struct {
	db    *sql.DB
	cache sync.Map // token string → *store.AgentIdentity
}

// Compile-time interface compliance checks.
// If any method is missing the build fails with a clear error.
var (
	_ store.AgentStore     = (*Store)(nil)
	_ store.SessionStore   = (*Store)(nil)
	_ store.UserStore      = (*Store)(nil)
	_ store.DashboardStore = (*Store)(nil)
	_ store.AlertsStore    = (*Store)(nil)
)
