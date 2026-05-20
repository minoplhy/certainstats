package dashboard

import (
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"database/sql"
	"fmt"
	"net/http"
)

// GET /api/dashboard
func GetDashboardHandler(dashboard store.DashboardStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)

		dashboardID := r.PathValue("id")
		if dashboardID == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Missing dashboard_id")
			return
		}

		row, err := dashboard.DashboardGetInfo(r.Context(), dashboardID, userID)
		if err == sql.ErrNoRows {
			apiresponse.Error(w, http.StatusNotFound, "dashboard not found")
			return
		}
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		var dashboard_out map[string]interface{}
		agents, err := dashboard.DashboardGetAgents(r.Context(), row.DashboardID, userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to get agents")
			fmt.Println(err)
			return
		}
		dashboard_out = map[string]interface{}{
			"dashboard_id": row.DashboardID,
			"slug":         row.Slug,
			"title":        row.Title,
			"access_control": row.AccessRules,
			"agents":       agents,
		}

		w.Header().Set("Content-Type", "application/json")
		apiresponse.JSON(w, http.StatusOK, dashboard_out)
	}
}
