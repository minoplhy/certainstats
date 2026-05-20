package dashboard

import (
	baseresponse "certainstats/internal/base/response"
	ctx "certainstats/internal/context"
	"certainstats/internal/dashboard/accessrules"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"encoding/json"
	"net/http"
	"strings"
)

// PUT /api/dashboard
func EditDashboardHandler(dashboard store.DashboardStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)

		if r.Method != http.MethodPut {
			apiresponse.Error(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// 1. Grab the immutable dashboard ID from the URL
		dashboardID := r.PathValue("id")
		if dashboardID == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Missing dashboard ID in URL")
			return
		}

		// 2. Decode the payload
		var req baseresponse.CreateDashboardReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid payload")
			return
		}

		// 3. Parse Access Control
		accessControl, err := accessrules.ParseRules(req.AccessControl)
		if err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Field Parse error")
			return
		}

		// 4. Build the Dashboard struct using the ID from the URL
		updateDashboard := store.Dashboard{
			DashboardID: dashboardID,
			UserID:      userID,
			Slug:        req.Slug,
			Title:       req.Title,
			AccessRules: accessControl,
		}

		// 5. Fire the update
		err = dashboard.DashboardUpdate(r.Context(), updateDashboard, req.Agents)
		if err != nil {
			if err.Error() == "dashboard not found or unauthorized" {
				apiresponse.Error(w, http.StatusForbidden, "Forbidden or Not Found")
				return
			}
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				apiresponse.Error(w, http.StatusConflict, "That dashboard slug is already in use")
				return
			}

			apiresponse.Error(w, http.StatusInternalServerError, "Failed to update dashboard")
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "success", "message": "Dashboard updated successfully"}`))
	}
}
