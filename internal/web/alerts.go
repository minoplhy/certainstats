package web

import (
	agentdata "certainstats/internal/agent_data"
	alertrules "certainstats/internal/alert"
	c "certainstats/internal/base/alert"
	"certainstats/internal/networkmonitor"
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

	monitors := []networkmonitor.Monitor{}
	for page := 1; ; page++ {
		items, total, e := h.Store.NetworkList(r.Context(), userID, store.NetworkListFilter{}, page, 100)
		if e != nil {
			http.Error(w, "Failed to load monitors", 500)
			return
		}
		monitors = append(monitors, items...)
		if len(monitors) >= total || len(items) == 0 {
			break
		}
	}
	// Collector rules watch what each collector type offers; the form only offers those.
	var options []c.SubjectOptions
	if h.AlertSubjects != nil {
		if options, err = h.AlertSubjects.AlertSubjects(r.Context(), userID); err != nil {
			http.Error(w, "Failed to load collector subjects", http.StatusInternalServerError)
			return
		}
	}
	collectorNames := map[string]string{}
	for _, o := range options {
		collectorNames[o.CollectorID] = o.CollectorName
	}
	pd := h.newPageData(r, "Alert System", "alerts", map[string]any{
		"Alerts":         alerts,
		"Monitors":       monitors,
		"Targets":        targets,
		"Agents":         agents,
		"SubjectSources": subjectSources(options),
		"CollectorNames": collectorNames,
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

	threshold, thresholdErr := strconv.ParseFloat(r.FormValue("threshold"), 64)
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
	monitors := r.Form["monitor_ids"]
	var subjects []c.Subject
	_, isSubject := c.SourceOf(triggerType)
	switch {
	case isSubject:
		var msg string
		if subjects, msg = subjectRule(r, triggerType, threshold, thresholdErr); msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		operator, duration, agents, monitors = c.OpGreaterThan, "0s", nil, nil
	case triggerType == c.TriggerTypeNetworkLoss:
		if thresholdErr != nil {
			http.Error(w, "Invalid loss threshold", http.StatusBadRequest)
			return
		}
		operator = c.OpGreaterThan
		duration = "1h"
		agents = nil
	default:
		monitors = nil
	}

	alertID := fmt.Sprintf("alert_%d_%s", time.Now().UnixMicro(), agentdata.GenerateRandomString(6))
	newAlert := store.Alert{
		AlertID:    alertID,
		UserID:     userID,
		MonitorIDs: monitors,
		Subjects:   subjects,
		Nickname:   nickname,
		Enabled:    enabled,
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
		code := http.StatusInternalServerError
		if _, ok := err.(interface{ Validation() bool }); ok {
			code = http.StatusBadRequest
		}
		http.Error(w, "Failed to create alert rule: "+err.Error(), code)
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

	threshold, thresholdErr := strconv.ParseFloat(r.FormValue("threshold"), 64)
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
	monitors := r.Form["monitor_ids"]
	var subjects []c.Subject
	_, isSubject := c.SourceOf(triggerType)
	switch {
	case isSubject:
		var msg string
		if subjects, msg = subjectRule(r, triggerType, threshold, thresholdErr); msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		operator, duration, agents, monitors = c.OpGreaterThan, "0s", nil, nil
	case triggerType == c.TriggerTypeNetworkLoss:
		if thresholdErr != nil {
			http.Error(w, "Invalid loss threshold", http.StatusBadRequest)
			return
		}
		operator = c.OpGreaterThan
		duration = "1h"
		agents = nil
	default:
		monitors = nil
	}

	updatedAlert := store.Alert{
		AlertID:    alertID,
		UserID:     userID,
		MonitorIDs: monitors,
		Subjects:   subjects,
		Nickname:   nickname,
		Enabled:    enabled,
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
		code := http.StatusInternalServerError
		if _, ok := err.(interface{ Validation() bool }); ok {
			code = http.StatusBadRequest
		}
		http.Error(w, "Failed to update alert rule: "+err.Error(), code)
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

	if err := h.Store.AlertDelete(r.Context(), alertID, userID); err != nil {
		http.Error(w, "Failed to persist change", 500)
		return
	}
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

	if err := h.Store.TargetDelete(r.Context(), targetID, userID); err != nil {
		http.Error(w, "Failed to persist change", 500)
		return
	}
	http.Redirect(w, r, h.PanelPath+"/alerts?tab=targets", http.StatusSeeOther)
}

// subjectRule reads a collector rule's subjects (form field "subjects",
// source/collector/scope[/name]) and checks its threshold. It returns the
// problem in words, or "".
func subjectRule(r *http.Request, t c.TriggerType, threshold float64, thresholdErr error) ([]c.Subject, string) {
	trig := c.Trigger{Type: t, Threshold: threshold}
	if thresholdErr != nil {
		return nil, "Invalid threshold"
	}
	if err := alertrules.ParseTrigger(&trig); err != nil {
		return nil, err.Error()
	}
	var subjects []c.Subject
	seen := map[string]bool{}
	for _, v := range r.Form["subjects"] {
		s, ok := c.ParseSubject(v)
		if !ok {
			return nil, "Invalid selection"
		}
		if !seen[s.Key()] {
			seen[s.Key()] = true
			subjects = append(subjects, s)
		}
	}
	return subjects, alertrules.CheckSubjects(t, 0, 0, subjects)
}

// subjectSourceView is one collector source on the Alerts page: its rule
// conditions and what each of the owner's collectors offers them.
type subjectSourceView struct {
	Name     string
	Label    string
	Empty    string
	Triggers []c.SubjectTrigger
	Options  []c.SubjectOptions
}

// subjectSources groups the registered sources' conditions with the options of
// the owner's collectors. Sources without a condition are left out.
func subjectSources(options []c.SubjectOptions) []subjectSourceView {
	var out []subjectSourceView
	for _, src := range c.SubjectSources() {
		v := subjectSourceView{Name: src.Name, Label: src.Label, Empty: src.Empty}
		for _, t := range c.SubjectTriggers() {
			if t.Source == src.Name {
				v.Triggers = append(v.Triggers, t)
			}
		}
		for _, o := range options {
			if o.Source == src.Name {
				v.Options = append(v.Options, o)
			}
		}
		if len(v.Triggers) > 0 {
			out = append(out, v)
		}
	}
	return out
}
