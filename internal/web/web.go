package web

import (
	"certainstats/internal/auth"
	ctx "certainstats/internal/context"
	"certainstats/internal/metrics"
	"certainstats/internal/store"
	"net/http"
	"time"
)

type WebHandler struct {
	Renderer   *TemplateRenderer
	Store      store.FullStore
	Cache      *metrics.RealtimeCache
	PanelPath  string
	PublicPath string
	StaticPath string
}

// Helper to extract user ID from context
func getUserID(r *http.Request) string {
	if val, ok := r.Context().Value(ctx.UserIDKey).(string); ok {
		return val
	}
	return ""
}

// Build standard PageData helper
func (h *WebHandler) newPageData(r *http.Request, title, activeNav string, data any) PageData {
	userID := getUserID(r)
	m := make(map[string]any)
	if data != nil {
		if mapAny, ok := data.(map[string]any); ok {
			for k, v := range mapAny {
				m[k] = v
			}
		} else if mapStr, ok := data.(map[string]string); ok {
			for k, v := range mapStr {
				m[k] = v
			}
		}
	}
	m["PanelPath"] = h.PanelPath
	m["PublicPath"] = h.PublicPath
	m["StaticPath"] = h.StaticPath

	return PageData{
		Title:         title,
		PanelPath:     h.PanelPath,
		PublicPath:    h.PublicPath,
		StaticPath:    h.StaticPath,
		ActiveNav:     activeNav,
		Authenticated: userID != "",
		Year:          time.Now().Year(),
		Data:          m,
	}
}

// RequireAuthWeb redirects unauthenticated requests to login page.
func (h *WebHandler) RequireAuthWeb(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_token")
		if err != nil {
			http.Redirect(w, r, h.PanelPath+"/login", http.StatusSeeOther)
			return
		}

		sess, err := h.Store.SessionGet(r.Context(), cookie.Value)
		if err != nil || time.Now().After(sess.ExpiresAt) {
			auth.ClearSessionCookie(w)
			http.Redirect(w, r, h.PanelPath+"/login", http.StatusSeeOther)
			return
		}

		ctxVal := r.Context()
		ctxVal = stdContextWithValue(ctxVal, ctx.UserIDKey, sess.UserID)
		next.ServeHTTP(w, r.WithContext(ctxVal))
	}
}

func stdContextWithValue(parent stdContext, key, val any) stdContext {
	return stdContextValue{parent: parent, key: key, val: val}
}

type stdContext interface {
	Deadline() (deadline time.Time, ok bool)
	Done() <-chan struct{}
	Err() error
	Value(key any) any
}

type stdContextValue struct {
	parent stdContext
	key    any
	val    any
}

func (c stdContextValue) Deadline() (time.Time, bool) { return c.parent.Deadline() }
func (c stdContextValue) Done() <-chan struct{}       { return c.parent.Done() }
func (c stdContextValue) Err() error                  { return c.parent.Err() }
func (c stdContextValue) Value(key any) any {
	if c.key == key {
		return c.val
	}
	return c.parent.Value(key)
}
