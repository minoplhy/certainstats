package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckPathCollision(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		bad  bool
	}{
		{"default", Config{PanelPath: "/", PublicPath: "/dashboard"}, false},
		{"public under root panel collides", Config{PanelPath: "/", PublicPath: "/alerts"}, true},
		{"nested public path collides", Config{PanelPath: "/", PublicPath: "/dashboards/public"}, true},
		{"public under subpath panel collides", Config{PanelPath: "/admin", PublicPath: "/admin/settings"}, true},
		{"public under subpath panel ok", Config{PanelPath: "/admin", PublicPath: "/admin/status"}, false},
		{"disjoint prefixes", Config{PanelPath: "/admin", PublicPath: "/alerts"}, false},
		{"same path", Config{PanelPath: "/x", PublicPath: "/x"}, false},
		{"separate hosts", Config{PanelHost: "a", PanelPath: "/", PublicHost: "b", PublicPath: "/alerts"}, false},
	}
	for _, c := range cases {
		err := checkPathCollision(&c.cfg)
		if (err != nil) != c.bad {
			t.Errorf("%s: got err=%v, want collision=%v", c.name, err, c.bad)
		}
	}
}

func TestHostRouter(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8")
	mark := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(name)) })
	}
	split := HostRouter("admin.example", "/", "status.example", "/", mark("panel"), mark("public"), mark("fallback"))
	legacy := HostRouter("", "/", "", "/dashboard", mark("panel"), mark("public"), mark("fallback"))
	sub := HostRouter("admin.example", "/admin", "status.example", "/status", mark("panel"), mark("public"), mark("fallback"))

	cases := []struct {
		name, host, xfh, remote, path, want string
		router                              http.Handler
	}{
		{"panel host", "admin.example", "", "1.2.3.4:1", "/login", "panel", split},
		{"public host", "status.example", "", "1.2.3.4:1", "/x", "public", split},
		{"untrusted forwarded host ignored", "status.example", "admin.example", "1.2.3.4:1", "/login", "public", split},
		{"trusted forwarded host honoured", "internal", "admin.example", "10.1.2.3:1", "/login", "panel", split},
		// main.go mounts nothing on the fallback when both hosts are set, so this 404s there.
		{"unknown host goes to fallback", "other.example", "", "1.2.3.4:1", "/login", "fallback", split},
		// main.go leaves the fallback empty in URL mode, so this is a 404 there.
		{"configured host outside its prefix", "status.example", "", "1.2.3.4:1", "/admin/login", "fallback", sub},
		{"host match ignores case", "ADMIN.Example:443", "", "1.2.3.4:1", "/login", "panel", split},
		{"no hosts uses legacy", "anything", "", "1.2.3.4:1", "/login", "fallback", legacy},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", c.path, nil)
		req.Host = c.host
		req.RemoteAddr = c.remote
		if c.xfh != "" {
			req.Header.Set("X-Forwarded-Host", c.xfh)
		}
		rec := httptest.NewRecorder()
		c.router.ServeHTTP(rec, req)
		got := rec.Body.String()
		if rec.Code == http.StatusNotFound {
			got = "404"
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
