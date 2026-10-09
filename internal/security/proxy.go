package security

import (
	"net"
	"net/http"
	"os"
	"strings"
)

func trusted(ip net.IP) bool {
	for _, s := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		_, n, e := net.ParseCIDR(strings.TrimSpace(s))
		if e == nil && n.Contains(ip) {
			return true
		}
	}
	return false
}
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "unknown"
	}
	if trusted(ip) {
		parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			next := net.ParseIP(strings.TrimSpace(parts[i]))
			if next == nil {
				break
			}
			ip = next
			if !trusted(ip) {
				break
			}
		}
	}
	return ip.String()
}

// Strip forwarded headers at the trust boundary, before cookies or origins use them.
func ProxyHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !trusted(net.ParseIP(host)) {
			for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "X-Forwarded-Proto", "X-Forwarded-Scheme", "X-Forwarded-Host"} {
				r.Header.Del(h)
			}
		}
		next.ServeHTTP(w, r)
	})
}
