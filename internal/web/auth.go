package web

import (
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/auth"
	log "certainstats/internal/logger"
	"certainstats/internal/store"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

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
			pd := h.newPageData(r, "Initial Setup", "", nil)
			pd.FlashError = "Invalid or expired setup token"
			h.Renderer.RenderHTTP(w, http.StatusBadRequest, "setup.html", pd)
			return
		}

		hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}

		userID := "usr_" + agentdata.GenerateRandomString(16)
		if err := h.Store.CreateUser(r.Context(), userID, username, string(hashed), true); err != nil {
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
		pd := h.newPageData(r, "Sign In", "", nil)
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

		user, err := h.Store.GetByUsername(r.Context(), username)
		if err != nil {
			pd := h.newPageData(r, "Sign In", "", nil)
			pd.FlashError = "Invalid username or password"
			h.Renderer.RenderHTTP(w, http.StatusUnauthorized, "login.html", pd)
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			pd := h.newPageData(r, "Sign In", "", nil)
			pd.FlashError = "Invalid username or password"
			h.Renderer.RenderHTTP(w, http.StatusUnauthorized, "login.html", pd)
			return
		}

		tokBytes := make([]byte, 32)
		rand.Read(tokBytes)
		tok := hex.EncodeToString(tokBytes)
		expiresAt := time.Now().Add(30 * 24 * time.Hour)

		h.Store.SessionCreate(r.Context(), store.Session{
			Token:           tok,
			UserID:          user.UserID,
			ExpiresAt:       expiresAt,
			CreatedAt:       time.Now(),
			LastConnectedAt: time.Now(),
			IPAddress:       r.RemoteAddr,
			UserAgent:       r.UserAgent(),
		})

		auth.SetSessionCookie(w, r, tok, expiresAt)
		http.Redirect(w, r, h.PanelPath+"/", http.StatusSeeOther)
	}
}

func (h *WebHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session_token"); err == nil {
		h.Store.SessionDelete(r.Context(), cookie.Value)
	}
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, h.PanelPath+"/login", http.StatusSeeOther)
}
