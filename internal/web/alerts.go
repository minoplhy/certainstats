package web

import (
	agentdata "certainstats/internal/agent_data"
	c "certainstats/internal/base/alert"
	"certainstats/internal/store"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (h *WebHandler) AlertsListHandler(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)

	alerts, err := h.Store.AlertList(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load alerts", http.StatusInternalServerError)
		return
	}

	targets, err := h.Store.TargetList(r.Context(), userID)
	if err != nil {
		targets = []c.AlertTarget{}
	}

	agents, err := h.Store.AgentList(r.Context(), userID)
	if err != nil {
		agents = []store.Agent{}
	}

	pd := h.newPageData(r, "Alert System", "alerts", map[string]any{
		"Alerts":  alerts,
		"Targets": targets,
		"Agents":  agents,
	})
	h.Renderer.RenderHTTP(w, http.StatusOK, "alerts_list.html", pd)
}

func (h *WebHandler) AlertCreateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	nickname := strings.TrimSpace(r.FormValue("nickname"))
	if nickname == "" {
		nickname = "Alert Rule"
	}
	enabled := r.FormValue("enabled") == "on" || r.FormValue("enabled") == "true" || r.FormValue("enabled") == "1"
	if _, ok := r.Form["enabled"]; !ok {
		enabled = true
	}

	triggerType := c.TriggerType(r.FormValue("trigger_type"))
	if triggerType == "" {
		triggerType = c.TriggerTypeCPU
	}

	operator := c.Operator(r.FormValue("operator"))
	if operator == "" {
		operator = c.OpGreaterThan
	}

	threshold, _ := strconv.ParseFloat(r.FormValue("threshold"), 64)
	duration := strings.TrimSpace(r.FormValue("duration"))
	if duration == "" {
		duration = "5m"
	}

	destType := c.DestinationType(r.FormValue("dest_type"))
	if destType == "" {
		destType = c.DestPreset
	}
	targetID := r.FormValue("target_id")
	destination := strings.TrimSpace(r.FormValue("destination"))
	payload := strings.TrimSpace(r.FormValue("payload"))

	agents := r.Form["agents"]

	alertID := fmt.Sprintf("alert_%d_%s", time.Now().UnixMicro(), agentdata.GenerateRandomString(6))
	newAlert := store.Alert{
		AlertID:  alertID,
		UserID:   userID,
		Nickname: nickname,
		Enabled:  enabled,
		Trigger: c.Trigger{
			Type:      triggerType,
			Operator:  operator,
			Threshold: threshold,
			Duration:  duration,
		},
		Action: c.AlertAction{
			Type:        destType,
			TargetID:    targetID,
			Destination: destination,
			Payload:     payload,
		},
	}

	for _, agID := range agents {
		if strings.TrimSpace(agID) != "" {
			newAlert.Agents = append(newAlert.Agents, c.AgentState{
				AgentID: strings.TrimSpace(agID),
				Status:  "ok",
			})
		}
	}

	err := h.Store.AlertCreate(r.Context(), newAlert)
	if err != nil {
		http.Error(w, "Failed to create alert rule: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.PanelPath+"/alerts", http.StatusSeeOther)
}

func (h *WebHandler) AlertUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	alertID := r.FormValue("id")
	if alertID == "" {
		alertID = chi.URLParam(r, "id")
	}
	if alertID == "" {
		http.Error(w, "Missing alert ID", http.StatusBadRequest)
		return
	}

	nickname := strings.TrimSpace(r.FormValue("nickname"))
	if nickname == "" {
		nickname = "Alert Rule"
	}
	enabled := r.FormValue("enabled") == "on" || r.FormValue("enabled") == "true" || r.FormValue("enabled") == "1"

	triggerType := c.TriggerType(r.FormValue("trigger_type"))
	if triggerType == "" {
		triggerType = c.TriggerTypeCPU
	}

	operator := c.Operator(r.FormValue("operator"))
	if operator == "" {
		operator = c.OpGreaterThan
	}

	threshold, _ := strconv.ParseFloat(r.FormValue("threshold"), 64)
	duration := strings.TrimSpace(r.FormValue("duration"))
	if duration == "" {
		duration = "5m"
	}

	destType := c.DestinationType(r.FormValue("dest_type"))
	if destType == "" {
		destType = c.DestPreset
	}
	targetID := r.FormValue("target_id")
	destination := strings.TrimSpace(r.FormValue("destination"))
	payload := strings.TrimSpace(r.FormValue("payload"))

	agents := r.Form["agents"]

	updatedAlert := store.Alert{
		AlertID:  alertID,
		UserID:   userID,
		Nickname: nickname,
		Enabled:  enabled,
		Trigger: c.Trigger{
			Type:      triggerType,
			Operator:  operator,
			Threshold: threshold,
			Duration:  duration,
		},
		Action: c.AlertAction{
			Type:        destType,
			TargetID:    targetID,
			Destination: destination,
			Payload:     payload,
		},
	}

	err := h.Store.AlertUpdate(r.Context(), updatedAlert, agents)
	if err != nil {
		http.Error(w, "Failed to update alert rule: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.PanelPath+"/alerts", http.StatusSeeOther)
}

func (h *WebHandler) AlertDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	alertID := r.FormValue("id")

	_ = h.Store.AlertDelete(r.Context(), alertID, userID)
	http.Redirect(w, r, h.PanelPath+"/alerts", http.StatusSeeOther)
}

func (h *WebHandler) TargetCreateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	name := strings.TrimSpace(r.FormValue("name"))
	targetType := r.FormValue("type")
	destination := strings.TrimSpace(r.FormValue("destination"))
	payload := strings.TrimSpace(r.FormValue("payload"))

	targetID := "tgt_" + agentdata.GenerateRandomString(16)
	err := h.Store.TargetCreate(r.Context(), c.AlertTarget{
		TargetID:    targetID,
		UserID:      userID,
		Name:        name,
		Type:        c.DestinationType(targetType),
		Destination: destination,
		Payload:     payload,
		CreatedAt:   time.Now(),
	})
	if err != nil {
		http.Error(w, "Failed to save target preset", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.PanelPath+"/alerts?tab=targets", http.StatusSeeOther)
}

func (h *WebHandler) TargetUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	targetID := r.FormValue("id")
	if targetID == "" {
		targetID = chi.URLParam(r, "id")
	}
	name := strings.TrimSpace(r.FormValue("name"))
	targetType := r.FormValue("type")
	destination := strings.TrimSpace(r.FormValue("destination"))
	payload := strings.TrimSpace(r.FormValue("payload"))

	err := h.Store.TargetUpdate(r.Context(), c.AlertTarget{
		TargetID:    targetID,
		UserID:      userID,
		Name:        name,
		Type:        c.DestinationType(targetType),
		Destination: destination,
		Payload:     payload,
	})
	if err != nil {
		http.Error(w, "Failed to update target: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.PanelPath+"/alerts?tab=targets", http.StatusSeeOther)
}

func (h *WebHandler) TargetDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	targetID := r.FormValue("id")

	_ = h.Store.TargetDelete(r.Context(), targetID, userID)
	http.Redirect(w, r, h.PanelPath+"/alerts?tab=targets", http.StatusSeeOther)
}
