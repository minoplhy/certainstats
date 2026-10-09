package security

import (
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	at     time.Time
}
type client struct {
	requests, builds bucket
	seen             time.Time
}
type Limiter struct {
	mu                                               sync.Mutex
	entries                                          map[string]*client
	overflow                                         client
	requestRate, buildRate                           float64
	requestBurst, buildBurst                         float64
	requestRejected, buildRejected, overflowRequests uint64
}

func envNumber(key string, fallback float64) float64 {
	n, e := strconv.ParseFloat(os.Getenv(key), 64)
	if e == nil && n > 0 && !math.IsInf(n, 0) && !math.IsNaN(n) {
		return n
	}
	return fallback
}
func NewLimiter() *Limiter {
	return &Limiter{entries: make(map[string]*client), requestRate: envNumber("PUBLIC_REQUESTS_PER_MINUTE", 1200) / 60, buildRate: envNumber("PUBLIC_BUILDS_PER_MINUTE", 60) / 60, requestBurst: envNumber("PUBLIC_REQUEST_BURST", 120), buildBurst: envNumber("PUBLIC_BUILD_BURST", 20)}
}

var PublicWaitSlots = make(chan struct{}, 64)

var PublicLimits = NewLimiter()

func (l *Limiter) Allow(ip string, build bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	c := l.entries[ip]
	if c == nil {
		if len(l.entries) >= 10000 {
			for k, v := range l.entries {
				if now.Sub(v.seen) > 10*time.Minute {
					delete(l.entries, k)
				}
			}
		}
		if len(l.entries) >= 10000 {
			l.overflowRequests++
			c = &l.overflow
		} else {
			c = &client{}
			l.entries[ip] = c
		}
	}
	c.seen = now
	b := &c.requests
	rate, burst := l.requestRate, l.requestBurst
	if build {
		b = &c.builds
		rate, burst = l.buildRate, l.buildBurst
	}
	if b.at.IsZero() {
		b.tokens = burst
	} else {
		b.tokens += now.Sub(b.at).Seconds() * rate
		if b.tokens > burst {
			b.tokens = burst
		}
	}
	b.at = now
	if b.tokens < 1 {
		if build {
			l.buildRejected++
		} else {
			l.requestRejected++
		}
		return false
	}
	b.tokens--
	return true
}
func Reject(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Retry-After", "2")
	http.Error(w, message, status)
}
func PublicRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !PublicLimits.Allow(ClientIP(r), false) {
			Reject(w, 429, "Request limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *Limiter) Statistics() map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	return map[string]any{"ips": len(l.entries), "request_rejected": l.requestRejected, "build_rejected": l.buildRejected, "overflow": l.overflowRequests}
}
