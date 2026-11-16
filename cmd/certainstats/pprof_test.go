package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStartPprofOffWhenUnset(t *testing.T) {
	srv, err := startPprof("")
	if srv != nil || err != nil {
		t.Fatalf("got %v %v, want nothing started", srv, err)
	}
}

func TestStartPprofRejectsNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:6060", ":6060", "192.0.2.1:6060", "example.com:6060", "6060"} {
		if srv, err := startPprof(addr); err == nil {
			srv.Close()
			t.Fatalf("%q accepted", addr)
		}
	}
}

func TestStartPprofServesHeap(t *testing.T) {
	srv, err := startPprof("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown(context.Background())
	resp, err := http.Get("http://" + srv.Addr + "/debug/pprof/heap?debug=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "heap profile") {
		t.Fatalf("status %d, body %.80q", resp.StatusCode, body)
	}
}
