package ws

import (
	"net/http/httptest"
	"testing"
)

func TestBrowserOriginDefaults(t *testing.T) {
	ConfigureOrigins("")
	r := httptest.NewRequest("GET", "https://monitor.example/api/ws", nil)
	for _, test := range []struct {
		origin  string
		allowed bool
	}{{"https://monitor.example", true}, {"https://evil.example", false}, {"", false}, {"null", false}, {"https://monitor.example/evil", false}} {
		r.Header.Set("Origin", test.origin)
		if allowed := checkRequestOrigin(r) == nil; allowed != test.allowed {
			t.Fatalf("origin %q allowed %v", test.origin, allowed)
		}
	}
	ConfigureOrigins("https://additional.example")
	defer ConfigureOrigins("")
	r.Header.Set("Origin", "https://additional.example")
	if err := checkRequestOrigin(r); err != nil {
		t.Fatal(err)
	}
}
