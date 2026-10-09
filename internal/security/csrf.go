package security

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
)

type csrfKey struct{}

var csrfSecret = func() []byte {
	v := make([]byte, 32)
	if _, e := rand.Read(v); e != nil {
		panic(e)
	}
	return v
}()

func sign(value string) string {
	h := hmac.New(sha256.New, csrfSecret)
	h.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func validToken(value string) bool {
	p := strings.Split(value, ".")
	return len(p) == 2 && len(p[0]) == 43 && hmac.Equal([]byte(p[1]), []byte(sign(p[0])))
}
func CSRFToken(r *http.Request) string { v, _ := r.Context().Value(csrfKey{}).(string); return v }
func BrowserProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasSuffix(r.URL.Path, "/submit") || strings.HasSuffix(r.URL.Path, "/api/beszel/agent-connect") {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		token := ""
		if c, e := r.Cookie("csrf_token"); e == nil && validToken(c.Value) {
			token = c.Value
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			provided := r.Header.Get("X-CSRF-Token")
			if provided == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
				if e := r.ParseForm(); e != nil {
					http.Error(w, "Invalid form", 400)
					return
				}
				provided = r.PostForm.Get("csrf_token")
			}
			if token == "" || !hmac.Equal([]byte(token), []byte(provided)) {
				http.Error(w, "Invalid CSRF token", 403)
				return
			}
		}
		// Public anonymous pages have no mutations and must remain shareable in the server cache.
		if token == "" {
			nonce := make([]byte, 32)
			if _, e := rand.Read(nonce); e != nil {
				http.Error(w, "Token generation failed", 500)
				return
			}
			v := base64.RawURLEncoding.EncodeToString(nonce)
			token = v + "." + sign(v)
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey{}, token)))
	})
}
