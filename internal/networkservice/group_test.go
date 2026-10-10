package networkservice

import (
	ctxkey "certainstats/internal/context"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGroupHTTPContracts(t *testing.T) {
	s, _ := fixture(t)
	call := func(method, target, body, user string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/network-monitors/group?target="+target, strings.NewReader(body))
		if user != "" {
			r = r.WithContext(context.WithValue(r.Context(), ctxkey.UserIDKey, user))
		}
		if method == "GET" {
			s.Group(w, r)
		} else {
			s.EditGroup(w, r)
		}
		return w
	}
	w := call("GET", "https%3A%2F%2Fexample.com", "", "owner")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var group struct {
		Revision string
		Items    []struct {
			ID string `json:"monitor_id"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &group); err != nil || group.Revision == "" || len(group.Items) != 1 {
		t.Fatal(group, err)
	}
	body := func(revision string, interval int, ids []string) string {
		b, _ := json.Marshal(map[string]any{"original_target": "https://example.com", "revision": revision, "target": "https://example.com", "protocol": "http", "interval_seconds": interval, "agent_ids": ids})
		return string(b)
	}
	for _, tc := range []struct {
		name, method, target, body, user string
		code                             int
	}{
		{"unauth read", "GET", "https%3A%2F%2Fexample.com", "", "", 401},
		{"foreign read", "GET", "https%3A%2F%2Fexample.com", "", "foreign", 404},
		{"missing", "GET", "missing", "", "owner", 404},
		{"unauth write", "PATCH", "", "{}", "", 401},
		{"foreign write", "PATCH", "", body(group.Revision, 300, []string{"node"}), "foreign", 404},
		{"malformed", "PATCH", "", "{", "owner", 400},
		{"unknown fields", "PATCH", "", `{"unexpected":true}`, "owner", 400},
		{"bad port", "PATCH", "", `{"port":65536}`, "owner", 400},
		{"bad interval", "PATCH", "", body(group.Revision, 5, []string{"node"}), "owner", 400},
		{"empty membership", "PATCH", "", body(group.Revision, 300, nil), "owner", 400},
		{"stale", "PATCH", "", body("old", 300, []string{"node"}), "owner", 409},
		{"success", "PATCH", "", body(group.Revision, 300, []string{"node"}), "owner", 200},
		{"revision invalidated", "PATCH", "", body(group.Revision, 300, []string{"node"}), "owner", 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(tc.method, tc.target, tc.body, tc.user)
			if w.Code != tc.code {
				t.Fatalf("code=%d body=%s", w.Code, w.Body)
			}
		})
	}
}
