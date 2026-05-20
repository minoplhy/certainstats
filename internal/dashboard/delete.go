package dashboard

import (
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"database/sql"
	"net/http"
)

func DeleteDashboardHandler(dashboard store.DashboardStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)
		dashboardID := r.PathValue("id")

		if r.Method != http.MethodDelete {
			apiresponse.Error(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		if dashboardID == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Missing dashboard_id")
			return
		}

		err := dashboard.DashboardDelete(r.Context(), dashboardID, userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Internal Error")
		}
		if err == sql.ErrNoRows {
			apiresponse.Error(w, http.StatusNotFound, "dashboard not found!")
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "success", "message": "Dashboard deleted successfully"}`))
	}
}
