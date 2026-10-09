package ws

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

var originMu sync.RWMutex
var allowedOrigins []string

// ConfigureOrigins runs after public/panel URL configuration has resolved.
func ConfigureOrigins(raw string) {
	originMu.Lock()
	defer originMu.Unlock()
	allowedOrigins = nil
	for _, o := range strings.Split(raw, ",") {
		u, err := url.Parse(strings.TrimSpace(o))
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil {
			allowedOrigins = append(allowedOrigins, u.Scheme+"://"+u.Host)
		}
	}
}
func checkOrigin(origin string) error {
	originMu.RLock()
	defer originMu.RUnlock()
	for _, o := range allowedOrigins {
		if o == origin {
			return nil
		}
	}
	return errors.New("unauthorized origin")
}
func checkRequestOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid origin")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	if u.Scheme == scheme && strings.EqualFold(u.Host, r.Host) {
		return nil
	}
	return checkOrigin(origin)
}
