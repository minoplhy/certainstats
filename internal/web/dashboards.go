package web

import (
	agentdata "certainstats/internal/agent_data"
	baseresponse "certainstats/internal/base/response"
	ctx "certainstats/internal/context"
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/store"
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
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

	pd := h.newPageData(r, "Edit Dashboard — "+dash.Title, "dashboards", map[string]any{
		"IsCreate":        false,
		"Dashboard":       dash,
		"AvailableAgents": availableAgents,
		"DashboardAgents": dashAgents,
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
	maxDays, _ := strconv.Atoi(r.FormValue("max_days"))
	if maxDays <= 0 {
		maxDays = 7
	}

	features := r.Form["features"]
	if len(features) == 0 {
		features = accessrules.FeaturesList
	}

	metricsList := r.Form["metrics"]
	if len(metricsList) == 0 {
		metricsList = accessrules.MetricsList
	}

	dashID := "dash_" + agentdata.GenerateRandomString(16)
	defaultRule := accessrules.AccessRules{
		"public": accessrules.AccessRule{
			AllowedFeatures: features,
			AllowedMetrics:  metricsList,
			MaxDays:         uint(maxDays),
		},
	}

	var reqAgents []baseresponse.CreateDashboardReqAgent
	agentsOrderRaw := r.FormValue("agents_order")
	isDragged := r.FormValue("is_dragged") == "1"

	selectedAgentsMap := make(map[string]bool)
	for _, aid := range r.Form["agents"] {
		selectedAgentsMap[strings.TrimSpace(aid)] = true
	}

	var orderedAgentIDs []string
	if agentsOrderRaw != "" {
		for _, aid := range strings.Split(agentsOrderRaw, ",") {
			aid = strings.TrimSpace(aid)
			if aid != "" && selectedAgentsMap[aid] {
				orderedAgentIDs = append(orderedAgentIDs, aid)
			}
		}
	} else {
		for _, aid := range r.Form["agents"] {
			aid = strings.TrimSpace(aid)
			if aid != "" {
				orderedAgentIDs = append(orderedAgentIDs, aid)
			}
		}
	}

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
		http.Redirect(w, r, h.PanelPath+"/dashboards?error=Failed+to+create+dashboard", http.StatusSeeOther)
		return
	}

	if len(reqAgents) > 0 {
		_ = h.Store.DashboardUpdate(r.Context(), newDash, reqAgents)
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
	maxDays, _ := strconv.Atoi(r.FormValue("max_days"))
	if maxDays <= 0 {
		maxDays = 7
	}

	features := r.Form["features"]
	if len(features) == 0 {
		features = accessrules.FeaturesList
	}

	metricsList := r.Form["metrics"]
	if len(metricsList) == 0 {
		metricsList = accessrules.MetricsList
	}

	defaultRule := accessrules.AccessRules{
		"public": accessrules.AccessRule{
			AllowedFeatures: features,
			AllowedMetrics:  metricsList,
			MaxDays:         uint(maxDays),
		},
	}

	var reqAgents []baseresponse.CreateDashboardReqAgent
	agentsOrderRaw := r.FormValue("agents_order")
	isDragged := r.FormValue("is_dragged") == "1"

	selectedAgentsMap := make(map[string]bool)
	for _, aid := range r.Form["agents"] {
		selectedAgentsMap[strings.TrimSpace(aid)] = true
	}

	var orderedAgentIDs []string
	if agentsOrderRaw != "" {
		for _, aid := range strings.Split(agentsOrderRaw, ",") {
			aid = strings.TrimSpace(aid)
			if aid != "" && selectedAgentsMap[aid] {
				orderedAgentIDs = append(orderedAgentIDs, aid)
			}
		}
	} else {
		for _, aid := range r.Form["agents"] {
			aid = strings.TrimSpace(aid)
			if aid != "" {
				orderedAgentIDs = append(orderedAgentIDs, aid)
			}
		}
	}

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
		http.Redirect(w, r, h.PanelPath+"/dashboard/"+dashID+"?error=Failed+to+update+dashboard", http.StatusSeeOther)
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
		_ = h.Store.DashboardDelete(r.Context(), dashID, userID)
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

	cacheKey := "html_dash_" + slug

	// 1. Unified Cache Check
	if entry, hit := ctx.GetCacheEntry(&ctx.DashboardHTMLCache, cacheKey); hit {
		entry.Serve(w, r, "text/html; charset=utf-8", http.StatusOK)
		return
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
	if !ok {
		rule = accessrules.AccessRule{
			AllowedFeatures: accessrules.FeaturesList,
			AllowedMetrics:  accessrules.MetricsList,
			MaxDays:         7,
		}
	}

	pubAgents, err := h.Store.DashboardGetPublicAgents(r.Context(), slug, rule)
	if err != nil {
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}

	pubStaticPath := h.PublicPath + "/static"
	if h.PublicPath == "/" {
		pubStaticPath = "/static"
	}

	pd := PageData{
		Title:      dash.Title,
		PublicPath: h.PublicPath,
		StaticPath: pubStaticPath,
		Year:       time.Now().Year(),
		Data: map[string]any{
			"Dashboard":   dash,
			"Agents":      pubAgents,
			"AccessRules": rule,
			"StaticPath":  pubStaticPath,
			"PublicPath":  h.PublicPath,
		},
	}

	var buf bytes.Buffer
	if err := h.Renderer.Render(&buf, "public_dashboard.html", pd); err != nil {
		http.Error(w, fmt.Sprintf("Template Error: %v", err), http.StatusInternalServerError)
		return
	}

	entry := ctx.NewCacheEntry(buf.Bytes(), ctx.DefaultCacheTTL)
	ctx.DashboardHTMLCache.Store(cacheKey, entry)

	entry.Serve(w, r, "text/html; charset=utf-8", http.StatusOK)
}
