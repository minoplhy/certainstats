package auth

import (
	"certainstats/internal/store"
	"certainstats/internal/response"
	"certainstats/internal/lifecycle"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"strings"
	"sync"
	"log"
)

var (
	setupToken string
	tokenMu    sync.RWMutex
)

func SetSetupToken(tok string) {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	setupToken = tok
}

func GetSetupToken() string {
	tokenMu.RLock()
	defer tokenMu.RUnlock()
	return setupToken
}

func ClearSetupToken() {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	setupToken = ""
}

func ValidateSetupToken(tok string) bool {
	savedToken := GetSetupToken()
	return secureTokenCompare(tok, savedToken)
}

type SetupRequest struct {
	Token           string `json:"token"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
}

func GetSetupStatusHandler(users store.UserStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		isZero, err := users.IsUserZero(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		response.JSON(w, http.StatusOK, map[string]bool{
			"setup_required": isZero,
		})
	}
}

func secureTokenCompare(a, b string) bool {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func CheckSetupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		savedToken := GetSetupToken()
		if !secureTokenCompare(token, savedToken) {
			response.Error(w, http.StatusNotFound, "Not Found")
			return
		}

		response.JSON(w, http.StatusOK, map[string]bool{
			"valid": true,
		})
	}
}

func RegisterFirstUserHandler(users store.UserStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req SetupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		savedToken := GetSetupToken()
		if !secureTokenCompare(req.Token, savedToken) {
			response.Error(w, http.StatusNotFound, "Not Found")
			return
		}

		if req.Username == "" || req.Password == "" {
			response.Error(w, http.StatusBadRequest, "Username and password cannot be empty")
			return
		}

		if req.Password != req.PasswordConfirm {
			response.Error(w, http.StatusBadRequest, "Passwords do not match")
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to hash password")
			return
		}

		// Generate dynamic 16-byte random userID: usr_<32 hex chars>
		uBytes := make([]byte, 16)
		if _, err := rand.Read(uBytes); err != nil {
			response.Error(w, http.StatusInternalServerError, "Failed to generate dynamic user ID")
			return
		}
		userID := "usr_" + hex.EncodeToString(uBytes)

		// Create as administrator (true)
		if err := users.CreateUser(r.Context(), userID, req.Username, string(hash), true); err != nil {
			if strings.Contains(err.Error(), "username already exists") {
				response.Error(w, http.StatusConflict, "Username is already taken")
				return
			}
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Clear setup token immediately (Dynamic Deregistration)
		ClearSetupToken()

		response.JSON(w, http.StatusOK, map[string]string{
			"status": "success",
		})
	}
}

func RestartServerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, http.StatusOK, map[string]string{
			"status": "restarting",
		})

		go func() {
			log.Println("[Setup] Restarting server as requested by setup administrator...")
			// Exit code 1 satisfies systemd/Docker on-failure/always restart policies cleanly
			lifecycle.TriggerRestart(1)
		}()
	}
}
