package web

import (
	"bytes"
	agentdata "certainstats/internal/agent_data"
	baseresponse "certainstats/internal/base/response"
	ctx "certainstats/internal/context"
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/store"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (h *WebHandler) DashboardsListHandler(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	dashboards, err := h.Store.DashboardList(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load dashboards", http.StatusInternalServerError)
		return
	}
	agents, _ := h.Store.AgentList(r.Context(), userID)

	pd := h.newPageData(r, "Public Dashboards", "dashboards", map[string]any{
		"Dashboards": dashboards,
		"Agents":     agents,
	})
	h.Renderer.RenderHTTP(w, http.StatusOK, "dashboards_list.html", pd)
}

func (h *WebHandler) DashboardCreatePageHandler(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	availableAgents, _ := h.Store.AgentList(r.Context(), userID)

	pd := h.newPageData(r, "Create Public Dashboard", "dashboards", map[string]any{
		"IsCreate":        true,
		"AvailableAgents": availableAgents,
		"Dashboard": store.Dashboard{
			AccessRules: accessrules.AccessRules{
				"public": accessrules.AccessRule{
					AllowedFeatures: accessrules.FeaturesList,
					AllowedMetrics:  accessrules.MetricsList,
					MaxDays:         7,
				},
			},
		},
		"DashboardAgents": []store.PublicAgentIdentity{},
	})
	h.Renderer.RenderHTTP(w, http.StatusOK, "dashboard_edit.html", pd)
}

func (h *WebHandler) DashboardEditHandler(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	dashID := chi.URLParam(r, "id")
	if dashID == "" {
		dashID = r.URL.Query().Get("id")
	}
	if dashID == "" {
		http.Redirect(w, r, h.PanelPath+"/dashboards", http.StatusSeeOther)
		return
	}

	dash, err := h.Store.DashboardGetInfo(r.Context(), dashID, userID)
	if err != nil {
		http.Redirect(w, r, h.PanelPath+"/dashboards?error=Dashboard+not+found", http.StatusSeeOther)
		return
	}

	dashAgents, _ := h.Store.DashboardGetAgents(r.Context(), dashID, userID)
	availableAgents, _ := h.Store.AgentList(r.Context(), userID)

	isDragged := false
	for _, a := range dashAgents {
		if a.SortKey != "" {
			isDragged = true
			break
		}
	}

	pd := h.newPageData(r, "Edit Dashboard — "+dash.Title, "dashboards", map[string]any{
		"IsCreate":        false,
		"Dashboard":       dash,
		"AvailableAgents": availableAgents,
		"DashboardAgents": dashAgents,
		"IsDragged":       isDragged,
	})
	h.Renderer.RenderHTTP(w, http.StatusOK, "dashboard_edit.html", pd)
}

func (h *WebHandler) DashboardCreateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	title := strings.TrimSpace(r.FormValue("title"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	}
	if title == "" {
		h.dashboardFormError(w, r, "Title cannot be empty.", "title", r.FormValue("id") == "")
		return
	}
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(slug) {
		h.dashboardFormError(w, r, "Use lowercase letters, numbers, and dashes for the slug.", "slug", r.FormValue("id") == "")
		return
	}
	maxDays, _ := strconv.Atoi(r.FormValue("max_days"))
	if maxDays <= 0 {
		maxDays = 7
	}

	features := r.Form["features"]

	metricsList := r.Form["metrics"]

	dashID := "dash_" + agentdata.GenerateRandomString(16)
	defaultRule := accessrules.AccessRules{
		"public": accessrules.AccessRule{
			AllowedFeatures: features,
			AllowedMetrics:  metricsList,
			MaxDays:         uint(maxDays),
		},
	}

	rawRules, _ := json.Marshal(defaultRule)
	if _, err := accessrules.ParseRules(string(rawRules)); err != nil {
		h.dashboardFormError(w, r, "Invalid public access settings: "+err.Error(), "max_days", r.FormValue("id") == "")
		return
	}

	var reqAgents []baseresponse.CreateDashboardReqAgent
	isDragged := r.FormValue("is_dragged") == "1"

	selectedAgentsMap := make(map[string]bool)
	for _, aid := range r.Form["agents"] {
		aid = strings.TrimSpace(aid)
		if aid != "" {
			selectedAgentsMap[aid] = true
		}
	}

	orderedAgentIDs := parseAgentsOrder(r, selectedAgentsMap)

	for i, agentID := range orderedAgentIDs {
		alias := strings.TrimSpace(r.FormValue("alias_" + agentID))
		if alias == "" {
			alias = "Server"
		}
		sortKey := ""
		if isDragged {
			sortKey = fmt.Sprintf("%08d", i)
		}
		reqAgents = append(reqAgents, baseresponse.CreateDashboardReqAgent{
			AgentID: agentID,
			Alias:   alias,
			SortKey: sortKey,
		})
	}

	newDash := store.Dashboard{
		DashboardID: dashID,
		UserID:      userID,
		Slug:        slug,
		Title:       title,
		AccessRules: defaultRule,
	}

	if err := h.Store.DashboardCreate(r.Context(), newDash); err != nil {
		h.dashboardFormError(w, r, "Could not create dashboard. Check that the slug is unique.", "slug", true)
		return
	}

	if len(reqAgents) > 0 {
		if err := h.Store.DashboardUpdate(r.Context(), newDash, reqAgents); err != nil {
			if err := h.Store.DashboardDelete(r.Context(), dashID, userID); err != nil {
				http.Error(w, "Dashboard not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to save dashboard agents", http.StatusBadRequest)
			return
		}
	}

	ctx.InvalidateDashboard(slug)

	http.Redirect(w, r, h.PanelPath+"/dashboards?success=Dashboard+created+successfully", http.StatusSeeOther)
}

func (h *WebHandler) DashboardUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	dashID := r.FormValue("id")
	if dashID == "" {
		http.Redirect(w, r, h.PanelPath+"/dashboards?error=Missing+dashboard+ID", http.StatusSeeOther)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	if title == "" {
		h.dashboardFormError(w, r, "Title cannot be empty.", "title", r.FormValue("id") == "")
		return
	}
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(slug) {
		h.dashboardFormError(w, r, "Use lowercase letters, numbers, and dashes for the slug.", "slug", r.FormValue("id") == "")
		return
	}
	maxDays, _ := strconv.Atoi(r.FormValue("max_days"))
	if maxDays <= 0 {
		maxDays = 7
	}

	features := r.Form["features"]

	metricsList := r.Form["metrics"]

	defaultRule := accessrules.AccessRules{
		"public": accessrules.AccessRule{
			AllowedFeatures: features,
			AllowedMetrics:  metricsList,
			MaxDays:         uint(maxDays),
		},
	}

	rawRules, _ := json.Marshal(defaultRule)
	if _, err := accessrules.ParseRules(string(rawRules)); err != nil {
		h.dashboardFormError(w, r, "Invalid public access settings: "+err.Error(), "max_days", r.FormValue("id") == "")
		return
	}

	var reqAgents []baseresponse.CreateDashboardReqAgent
	isDragged := r.FormValue("is_dragged") == "1"

	selectedAgentsMap := make(map[string]bool)
	for _, aid := range r.Form["agents"] {
		aid = strings.TrimSpace(aid)
		if aid != "" {
			selectedAgentsMap[aid] = true
		}
	}

	orderedAgentIDs := parseAgentsOrder(r, selectedAgentsMap)

	for i, agentID := range orderedAgentIDs {
		alias := strings.TrimSpace(r.FormValue("alias_" + agentID))
		if alias == "" {
			alias = "Server"
		}
		sortKey := ""
		if isDragged {
			sortKey = fmt.Sprintf("%08d", i)
		}
		reqAgents = append(reqAgents, baseresponse.CreateDashboardReqAgent{
			AgentID: agentID,
			Alias:   alias,
			SortKey: sortKey,
		})
	}

	updateDashboard := store.Dashboard{
		DashboardID: dashID,
		UserID:      userID,
		Slug:        slug,
		Title:       title,
		AccessRules: defaultRule,
	}

	if err := h.Store.DashboardUpdate(r.Context(), updateDashboard, reqAgents); err != nil {
		h.dashboardFormError(w, r, "Could not update dashboard.", "slug", false)
		return
	}

	ctx.InvalidateDashboard(slug)

	http.Redirect(w, r, h.PanelPath+"/dashboards?success=Dashboard+updated+successfully", http.StatusSeeOther)
}

func (h *WebHandler) DashboardDeleteHandler(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	userID := getUserID(r)
	dashID := chi.URLParam(r, "id")
	if dashID == "" {
		dashID = r.FormValue("id")
	}

	if dashID != "" {
		if err := h.Store.DashboardDelete(r.Context(), dashID, userID); err != nil {
			http.Error(w, "Dashboard not found", http.StatusNotFound)
			return
		}
	}

	ctx.InvalidateDashboard("")

	if r.Method == http.MethodDelete || strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success"}`))
		return
	}

	http.Redirect(w, r, h.PanelPath+"/dashboards?success=Dashboard+deleted+successfully", http.StatusSeeOther)
}

func (h *WebHandler) PublicDashboardHandler(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		slug = chi.URLParam(r, "pub_id")
	}
	if slug == "" {
		clean := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(clean, "/")
		if len(parts) > 0 {
			slug = parts[len(parts)-1]
		}
	}

	dashPtr, err := h.Store.DashboardGetBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Dashboard Not Found", http.StatusNotFound)
			return
		}
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}
	dash := *dashPtr

	rule, ok := dash.AccessRules["public"]
	if !ok || rule.IsEmpty() {
		http.Error(w, "Public access disabled", http.StatusForbidden)
		return
	}

	cacheKey := fmt.Sprintf("html:%s:%d:%s:%s:%s", dash.DashboardID, dash.Version, slug, h.PublicPath, h.StaticPath)
	if entry, hit := ctx.GetCacheEntry(&ctx.DashboardHTMLCache, cacheKey); hit {
		entry.Serve(w, r, "text/html; charset=utf-8", http.StatusOK)
		return
	}
	finish, leader := ctx.BeginBuild(w, r, cacheKey, h.PublicDashboardHandler)
	if !leader {
		return
	}
	defer finish()

	pubAgents, err := h.Store.DashboardGetPublicAgents(r.Context(), slug, rule)
	if err != nil {
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}

	pubStaticPath := h.PublicPath + "/static"

	online := 0
	for _, a := range pubAgents {
		if a.IsOnline != nil && *a.IsOnline {
			online++
		}
	}

	pd := PageData{
		Title:      dash.Title,
		PublicPath: h.PublicPath,
		StaticPath: pubStaticPath,
		Year:       time.Now().Year(),
		Data: map[string]any{
			"Dashboard":    dash,
			"Agents":       pubAgents,
			"OnlineCount":  online,
			"OfflineCount": len(pubAgents) - online,
			"AccessRules":  rule,
			"StaticPath":   pubStaticPath,
			"PublicPath":   h.PublicPath,
		},
	}

	var buf bytes.Buffer
	if err := h.Renderer.Render(&buf, "public_dashboard.html", pd); err != nil {
		http.Error(w, fmt.Sprintf("Template Error: %v", err), http.StatusInternalServerError)
		return
	}

	entry := ctx.NewCacheEntry(buf.Bytes(), ctx.DefaultCacheTTL)
	current, err := h.Store.DashboardGetBySlug(r.Context(), slug)
	if err != nil || current.Version != dash.Version {
		http.Error(w, "Dashboard changed; retry", http.StatusConflict)
		return
	}
	ctx.DashboardHTMLCache.Store(cacheKey, entry)

	entry.Serve(w, r, "text/html; charset=utf-8", http.StatusOK)
}

func parseAgentsOrder(r *http.Request, selectedAgentsMap map[string]bool) []string {
	var orderedAgentIDs []string
	seen := make(map[string]bool)

	parseItem := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		var jsonArr []string
		if err := json.Unmarshal([]byte(raw), &jsonArr); err == nil {
			for _, aid := range jsonArr {
				aid = strings.TrimSpace(aid)
				if aid != "" && selectedAgentsMap[aid] && !seen[aid] {
					orderedAgentIDs = append(orderedAgentIDs, aid)
					seen[aid] = true
				}
			}
			return
		}
		for _, aid := range strings.Split(raw, ",") {
			aid = strings.TrimSpace(aid)
			if aid != "" && selectedAgentsMap[aid] && !seen[aid] {
				orderedAgentIDs = append(orderedAgentIDs, aid)
				seen[aid] = true
			}
		}
	}

	for _, raw := range r.Form["agents_order"] {
		parseItem(raw)
	}
	if len(orderedAgentIDs) == 0 {
		for _, raw := range r.Form["agents_order[]"] {
			parseItem(raw)
		}
	}

	for _, aid := range r.Form["agents"] {
		aid = strings.TrimSpace(aid)
		if aid != "" && selectedAgentsMap[aid] && !seen[aid] {
			orderedAgentIDs = append(orderedAgentIDs, aid)
			seen[aid] = true
		}
	}

	return orderedAgentIDs
}

func (h *WebHandler) dashboardFormError(w http.ResponseWriter, r *http.Request, message, field string, create bool) {
	days, _ := strconv.Atoi(r.FormValue("max_days"))
	if days <= 0 {
		days = 7
	}
	dash := store.Dashboard{DashboardID: r.FormValue("id"), Title: r.FormValue("title"), Slug: r.FormValue("slug"), AccessRules: accessrules.AccessRules{"public": {AllowedFeatures: r.Form["features"], AllowedMetrics: r.Form["metrics"], MaxDays: uint(days)}}}
	available, err := h.Store.AgentList(r.Context(), getUserID(r))
	if err != nil {
		http.Error(w, "Could not reload form", 500)
		return
	}
	var assigned []store.PublicAgentIdentity
	for i, id := range r.Form["agents"] {
		key := ""
		if r.FormValue("is_dragged") == "1" {
			key = fmt.Sprintf("%08d", i)
		}
		assigned = append(assigned, store.PublicAgentIdentity{AgentID: id, PublicAgentNickname: r.FormValue("alias_" + id), SortKey: key})
	}
	pd := h.newPageData(r, "Dashboard settings", "dashboards", map[string]any{"Dashboard": dash, "IsCreate": create, "IsDragged": r.FormValue("is_dragged") == "1", "AvailableAgents": available, "DashboardAgents": assigned, "Errors": map[string]string{field: message}})
	pd.FlashError = message
	h.Renderer.RenderHTTP(w, 400, "dashboard_edit.html", pd)
}
