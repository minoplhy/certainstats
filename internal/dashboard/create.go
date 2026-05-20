package dashboard

import (
	agentdata "certainstats/internal/agent_data"
	baseresponse "certainstats/internal/base/response"
	ctx "certainstats/internal/context"
	"certainstats/internal/dashboard/accessrules"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"encoding/json"
	"fmt"
	"net/http"
)

// POST /api/dashboards
func CreateDashboardHandler(dashboard store.DashboardStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)

		// 1. Ensure it's a POST request
		if r.Method != http.MethodPost {
			apiresponse.Error(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// 2. Define the struct that perfectly matches your React payload
		var CreateDashboardReq baseresponse.CreateDashboardReq

		if err := json.NewDecoder(r.Body).Decode(&CreateDashboardReq); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid payload")
			return
		}

		// 3. Convert the allowed fields array into a JSON string for SQLite storage
		//allowedFieldsJSON, err := json.Marshal(req.AllowedFields)
		accessControl, err := accessrules.ParseRules(CreateDashboardReq.AccessControl)
		if err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Field Parse error")
			return
		}
		dashboardID := fmt.Sprintf("das_%s", agentdata.GenerateRandomString(32))

		storeDashboard := store.Dashboard{
			DashboardID: dashboardID,
			UserID:      userID,
			Slug:        CreateDashboardReq.Slug,
			Title:       CreateDashboardReq.Title,
			AccessRules: accessControl,
		}
		err = dashboard.DashboardCreate(r.Context(), storeDashboard)
		if err != nil {
			// If slug is not unique, SQLite will throw an error here.
			apiresponse.Error(w, http.StatusConflict, "Failed to create dashboard ")
			return
		}

		// 6. Insert each selected agent into the mapping table with a secure public_id
		err = dashboard.DashboardAddAgents(r.Context(), storeDashboard, CreateDashboardReq.Agents)
		if err != nil {
			apiresponse.Error(w, http.StatusConflict, "Failed to add Agents ")
			return
		}
		// 8. Return success!
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"status": "success", "dashboard_id": "` + dashboardID + `"}`))
	}
}
