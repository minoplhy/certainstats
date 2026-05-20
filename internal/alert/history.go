package alert

import (
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	ctx "certainstats/internal/context"
	basealert "certainstats/internal/base/alert"
	"net/http"
	"strconv"
)

func HistoryAlertHandler(db store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)

		limitStr := r.URL.Query().Get("limit")
		limit := 25
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}

		pageStr := r.URL.Query().Get("page")
		page := 1
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}

		history, total, err := db.AlertHistoryListPaginated(r.Context(), userID, page, limit)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to retrieve alert history")
			return
		}

		if history == nil {
			history = []basealert.AlertHistory{}
		}

		totalPages := (total + limit - 1) / limit
		if totalPages < 0 {
			totalPages = 0
		}

		apiresponse.JSON(w, http.StatusOK, map[string]interface{}{
			"data":        history,
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		})
	}
}
