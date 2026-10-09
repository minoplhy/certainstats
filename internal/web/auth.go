package web

import (
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/auth"
	log "certainstats/internal/logger"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

func (h *WebHandler) SetupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		token := r.URL.Query().Get("token")
		pd := h.newPageData(r, "Initial Setup", "", map[string]string{"Token": token})
		h.Renderer.RenderHTTP(w, http.StatusOK, "setup.html", pd)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form body", http.StatusBadRequest)
			return
		}

		token := r.FormValue("token")
		username := r.FormValue("username")
		password := r.FormValue("password")

		if !auth.ValidateSetupToken(token) {
			pd := h.newPageData(r, "Initial Setup", "", map[string]any{"Token": token, "Username": username})
			pd.FlashError = "Invalid or expired setup token"
			h.Renderer.RenderHTTP(w, http.StatusBadRequest, "setup.html", pd)
			return
		}

		if username == "" {
			h.setupError(w, r, "username", "Username cannot be empty", 400)
			return
		}
		if err := auth.ValidatePassword(password, r.FormValue("confirm_password")); err != nil {
			h.setupError(w, r, "password", err.Error(), 400)
			return
		}

		hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}

		userID := "usr_" + agentdata.GenerateRandomString(16)
		if err := auth.CreateInitialUser(r.Context(), h.Store, userID, username, string(hashed)); err != nil {
			log.Printf("create user: %v", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}

		auth.ClearSetupToken()
		http.Redirect(w, r, h.PanelPath+"/login", http.StatusSeeOther)
	}
}

func (h *WebHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		pd := h.newPageData(r, "Sign In", "", map[string]any{"Username": r.FormValue("username")})
		h.Renderer.RenderHTTP(w, http.StatusOK, "login.html", pd)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}

		username := r.FormValue("username")
		password := r.FormValue("password")
		remember := r.FormValue("remember") == "true" || r.FormValue("remember") == "on" || r.FormValue("remember") == "1"

		user, err := h.Store.GetByUsername(r.Context(), username)
		if err != nil {
			pd := h.newPageData(r, "Sign In", "", map[string]any{"Username": r.FormValue("username")})
			pd.FlashError = "Invalid username or password"
			h.Renderer.RenderHTTP(w, http.StatusUnauthorized, "login.html", pd)
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			pd := h.newPageData(r, "Sign In", "", map[string]any{"Username": r.FormValue("username")})
			pd.FlashError = "Invalid username or password"
			h.Renderer.RenderHTTP(w, http.StatusUnauthorized, "login.html", pd)
			return
		}

		if err := auth.CreateBrowserSession(w, r, h.Store, user.UserID, remember); err != nil {
			h.loginError(w, r, "Failed to create session", 500)
			return
		}

		http.Redirect(w, r, h.PanelPath+"/", http.StatusSeeOther)
	}
}

func (h *WebHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session_token"); err == nil {
		if err := h.Store.SessionDelete(r.Context(), cookie.Value); err != nil {
			http.Error(w, "Logout failed; retry", 500)
			return
		}
	}
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, h.PanelPath+"/login", http.StatusSeeOther)
}

func (h *WebHandler) loginError(w http.ResponseWriter, r *http.Request, message string, status int) {
	pd := h.newPageData(r, "Sign In", "", map[string]any{"Username": r.FormValue("username")})
	pd.FlashError = message
	h.Renderer.RenderHTTP(w, status, "login.html", pd)
}

func (h *WebHandler) setupError(w http.ResponseWriter, r *http.Request, field, message string, status int) {
	pd := h.newPageData(r, "Initial Setup", "", map[string]any{"Token": r.FormValue("token"), "Username": r.FormValue("username"), "FieldErrors": map[string]string{field: message}})
	pd.FlashError = message
	h.Renderer.RenderHTTP(w, status, "setup.html", pd)
}
