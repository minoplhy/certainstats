package main

import "testing"

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
