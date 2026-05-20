package dashboard

import (
	ctx "certainstats/internal/context"
	"certainstats/internal/dashboard/accessrules"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// GET /api/public/dashboard
func PublicDashboardHandler(dashboard store.DashboardStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("pub_id")
		if slug == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Missing slug")
			return
		}

		// Cache hit
		if val, ok := ctx.DashboardCache.Load(slug); ok {
			entry := val.(*ctx.CacheEntry)
			if time.Now().Before(entry.ExpiresAt) {
				w.Header().Set("Content-Type", "application/json")
				w.Write(entry.Payload)
				return
			}
			ctx.DashboardCache.Delete(slug)
		}

		dash, err := dashboard.DashboardGetBySlug(r.Context(), slug)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				apiresponse.Error(w, http.StatusNotFound, "Dashboard not found")
				return
			}
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		rule, ok := dash.AccessRules[accessrules.PUBLIC]
		if !ok || rule.IsEmpty() {
			apiresponse.Error(w, http.StatusForbidden, "No public access rule configured")
			return
		}

		agents, err := dashboard.DashboardGetPublicAgents(r.Context(), slug, rule)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		payload, err := json.Marshal(map[string]any{
			"title":           dash.Title,
			"dashboard_id":    dash.DashboardID,
			"allowed_metrics": rule.AllowedMetrics,
			"max_days":        rule.MaxDays,
			"agents":          agents,
		})
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Serialisation error")
			return
		}

		ctx.DashboardCache.Store(slug, &ctx.CacheEntry{
			Payload:   payload,
			ExpiresAt: time.Now().Add(60 * time.Second),
		})

		w.Header().Set("Content-Type", "application/json")
		w.Write(payload)
	}
}
