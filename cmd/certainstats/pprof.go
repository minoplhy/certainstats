package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

// startPprof serves Go profiles on addr when PPROF_ADDR is set. It is off by
// default and only binds loopback, on its own mux apart from the app routes.
func startPprof(addr string) (*http.Server, error) {
	if addr == "" {
		return nil, nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("PPROF_ADDR must be host:port, such as 127.0.0.1:6060")
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("PPROF_ADDR must use a loopback host (127.0.0.1, ::1 or localhost)")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("PPROF_ADDR: %w", err)
	}
	srv := &http.Server{Addr: ln.Addr().String(), Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	return srv, nil
}
