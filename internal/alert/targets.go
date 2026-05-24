package alert

import (
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/base/alert"
	ctx "certainstats/internal/context"
	"certainstats/internal/notifications"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type CreateTargetRequest struct {
	Name        string                `json:"name"`
	Type        alert.DestinationType `json:"type"`
	Destination string                `json:"destination"`
	Payload     string                `json:"payload"`
}

// ListTargetsHandler handles GET /api/alerts/targets
func ListTargetsHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		targets, err := s.TargetList(r.Context(), userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			log.Println("ListTargets error:", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		apiresponse.JSON(w, http.StatusOK, targets)
	}
}

// CreateTargetHandler handles POST /api/alerts/targets
func CreateTargetHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		var req CreateTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if strings.TrimSpace(req.Name) == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Target name is required")
			return
		}
		if req.Type != alert.DestWebhook && req.Type != alert.DestDiscord {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid target type")
			return
		}
		if strings.TrimSpace(req.Destination) == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Destination URL is required")
			return
		}

		newTarget := alert.AlertTarget{
			TargetID:    fmt.Sprintf("trg_%d_%s", time.Now().UnixMicro(), agentdata.GenerateRandomString(6)),
			UserID:      userID,
			Name:        req.Name,
			Type:        req.Type,
			Destination: req.Destination,
			Payload:     req.Payload,
		}

		if err := s.TargetCreate(r.Context(), newTarget); err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			log.Println("CreateTarget error:", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		apiresponse.JSON(w, http.StatusOK, newTarget)
	}
}

// GetTargetHandler handles GET /api/alerts/targets/{id}
func GetTargetHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		targetID := chi.URLParam(r, "id")
		target, err := s.TargetGetByID(r.Context(), targetID, userID)
		if err != nil {
			apiresponse.Error(w, http.StatusNotFound, "Target not found")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		apiresponse.JSON(w, http.StatusOK, target)
	}
}

// EditTargetHandler handles PUT /api/alerts/targets/{id}
func EditTargetHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		targetID := chi.URLParam(r, "id")
		var req CreateTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if strings.TrimSpace(req.Name) == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Target name is required")
			return
		}
		if req.Type != alert.DestWebhook && req.Type != alert.DestDiscord {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid target type")
			return
		}
		if strings.TrimSpace(req.Destination) == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Destination URL is required")
			return
		}

		target, err := s.TargetGetByID(r.Context(), targetID, userID)
		if err != nil {
			apiresponse.Error(w, http.StatusNotFound, "Target not found")
			return
		}

		target.Name = req.Name
		target.Type = req.Type
		target.Destination = req.Destination
		target.Payload = req.Payload

		if err := s.TargetUpdate(r.Context(), target); err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			log.Println("UpdateTarget error:", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		apiresponse.JSON(w, http.StatusOK, target)
	}
}

// DeleteTargetHandler handles DELETE /api/alerts/targets/{id}
func DeleteTargetHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		targetID := chi.URLParam(r, "id")
		if err := s.TargetDelete(r.Context(), targetID, userID); err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			log.Println("DeleteTarget error:", err)
			return
		}

		apiresponse.Success(w, "Alert target deleted successfully", nil)
	}
}

type TestTargetRequest struct {
	TargetID    string                `json:"target_id,omitempty"`
	Type        alert.DestinationType `json:"type"`
	Destination string                `json:"destination"`
	Payload     string                `json:"payload"`
}

// TestTargetHandler handles POST /api/alerts/targets/test
func TestTargetHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		var req TestTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		// Resolve destination config
		var action alert.AlertAction
		if req.TargetID != "" {
			target, err := s.TargetGetByID(r.Context(), req.TargetID, userID)
			if err != nil {
				apiresponse.Error(w, http.StatusNotFound, "Target not found")
				return
			}
			action.Type = target.Type
			action.Destination = target.Destination
			action.Payload = target.Payload
		} else {
			if strings.TrimSpace(req.Destination) == "" {
				apiresponse.Error(w, http.StatusBadRequest, "Destination is required")
				return
			}
			action.Type = req.Type
			action.Destination = req.Destination
			action.Payload = req.Payload
		}

		// Dispatch a dummy notification using resolved action
		err := notifications.DispatchNotification(action, notifications.NotificationContext{
			AgentID:     "trg_test123",
			Nickname:    "Target Test Node",
			TriggerType: "preset_alert_target_test",
			Status:      "TEST",
			Value:       100.0,
		})
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Connection test failed: "+err.Error())
			return
		}

		apiresponse.Success(w, "Test notification sent successfully", nil)
	}
}
