package dashboard

import (
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"net/http"
)

// GET /api/dashboards
func ListDashboardsHandler(dashboard store.DashboardStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)

		rows, err := dashboard.DashboardList(r.Context(), userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		var dashboards []map[string]interface{}
		for _, j := range rows {

			dashboards = append(dashboards, map[string]interface{}{
				"dashboard_id": j.DashboardID,
				"slug":         j.Slug,
				"title":        j.Title,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		apiresponse.JSON(w, http.StatusOK, dashboards)
	}
}
