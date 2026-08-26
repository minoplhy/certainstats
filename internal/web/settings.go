package web

import (
	"certainstats/internal/auth"
	"certainstats/internal/store"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

func (h *WebHandler) SettingsHandler(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	sessions, err := h.Store.SessionListByUser(r.Context(), userID)
	if err != nil {
		sessions = []store.Session{}
	}

	currentToken := ""
	if cookie, err := r.Cookie("session_token"); err == nil {
		currentToken = cookie.Value
	}

	pd := h.newPageData(r, "Account Settings", "settings", map[string]any{
		"Sessions":     sessions,
		"CurrentToken": currentToken,
	})
	h.Renderer.RenderHTTP(w, http.StatusOK, "settings.html", pd)
}

func (h *WebHandler) PasswordChangeHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	oldPassword := r.FormValue("old_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	if newPassword != confirmPassword {
		http.Error(w, "New passwords do not match", http.StatusBadRequest)
		return
	}

	user, err := h.Store.GetByID(r.Context(), userID)
	if err != nil {
		http.Error(w, "User error", http.StatusInternalServerError)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		http.Error(w, "Current password incorrect", http.StatusUnauthorized)
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Encryption failure", http.StatusInternalServerError)
		return
	}

	_ = h.Store.UpdatePassword(r.Context(), userID, string(hashed))
	http.Redirect(w, r, h.PanelPath+"/settings", http.StatusSeeOther)
}

func (h *WebHandler) SessionEjectHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	tokenPrefix := r.FormValue("token_prefix")
	if tokenPrefix == "" {
		http.Redirect(w, r, h.PanelPath+"/settings", http.StatusSeeOther)
		return
	}

	list, err := h.Store.SessionListByUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var targetToken string
	currentToken := ""
	if cookie, err := r.Cookie("session_token"); err == nil {
		currentToken = cookie.Value
	}

	for _, s := range list {
		if s.TokenPrefix() == tokenPrefix || s.Token == tokenPrefix {
			targetToken = s.Token
			break
		}
	}

	if targetToken == "" {
		http.Redirect(w, r, h.PanelPath+"/settings", http.StatusSeeOther)
		return
	}

	_ = h.Store.SessionDelete(r.Context(), targetToken)

	if targetToken == currentToken {
		auth.ClearSessionCookie(w)
		http.Redirect(w, r, h.PanelPath+"/login", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, h.PanelPath+"/settings", http.StatusSeeOther)
}

func (h *WebHandler) SessionEjectOtherHandler(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	cookie, err := r.Cookie("session_token")
	if err == nil && cookie.Value != "" {
		_ = h.Store.SessionDeleteOther(r.Context(), userID, cookie.Value)
	}
	http.Redirect(w, r, h.PanelPath+"/settings", http.StatusSeeOther)
}
