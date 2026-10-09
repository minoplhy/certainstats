package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRF(t *testing.T) {
	var token string
	handler := BrowserProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { token = CSRFToken(r); w.WriteHeader(204) }))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || token == "" {
		t.Fatal("missing signed token")
	}
	for _, test := range []struct {
		value  string
		status int
	}{{"", 403}, {token + "x", 403}, {token, 204}} {
		r := httptest.NewRequest("POST", "/login", strings.NewReader("csrf_token="+test.value))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(cookies[0])
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("CSRF response %d expected %d", w.Code, test.status)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/submit", nil))
	if w.Code != 204 {
		t.Fatal("agent ingestion must be exempt")
	}
}
func TestTrustedProxy(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.1:42"
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	r.Header.Set("X-Forwarded-Proto", "https")
	ProxyHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ClientIP(r) != "192.0.2.1" || r.Header.Get("X-Forwarded-Proto") != "" {
			t.Fatal("untrusted proxy accepted")
		}
	})).ServeHTTP(httptest.NewRecorder(), r)
	r.RemoteAddr = "10.0.0.1:42"
	r.Header.Set("X-Forwarded-For", "spoof, 203.0.113.1, 10.0.0.2")
	if ip := ClientIP(r); ip != "203.0.113.1" {
		t.Fatalf("wrong trusted chain: %s", ip)
	}
}
func TestLimiterBudgetAndBound(t *testing.T) {
	limiter := NewLimiter()
	limiter.requestBurst = 2
	limiter.buildBurst = 1
	if !limiter.Allow("a", false) || !limiter.Allow("a", false) || limiter.Allow("a", false) {
		t.Fatal("request quota incorrect")
	}
	if !limiter.Allow("a", true) || limiter.Allow("a", true) {
		t.Fatal("build quota incorrect")
	}
	for i := 0; i < 11000; i++ {
		limiter.Allow(strings.Repeat("x", i%32)+string(rune(i)), false)
	}
	if len(limiter.entries) > 10000 {
		t.Fatal("unbounded limiter state")
	}
}
