package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"certainstats/internal/agent"
	"certainstats/internal/agent_parser/registry"
	"certainstats/internal/alert"
	"certainstats/internal/auth"
	b_ctx "certainstats/internal/context"
	"certainstats/internal/dashboard"
	"certainstats/internal/metrics"
	"certainstats/internal/routine"
	"certainstats/internal/store/sqlite"
	"certainstats/internal/ws"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/prometheus/tsdb"
	_ "modernc.org/sqlite"
)

var (
	name      = "certainstats"
	version   = "dev"
	commit    = ""
	buildTime = ""
)

func getRoutePrefix(envKey, defaultPath string) string {
	path := os.Getenv(envKey)
	if path == "" {
		path = defaultPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}
	return path
}

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-V" {
			printVersion()
			os.Exit(0)
		}
	}

	var err error

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}
	os.MkdirAll(dataDir, 0o755)

	// 1. SQLite
	db, err := sqlite.New(filepath.Join(dataDir, "agent_state.db"))
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	// 2. TSDB
	tsdbPath := filepath.Join(dataDir, "tsdb")
	os.MkdirAll(tsdbPath, 0o755)
	opts := tsdb.DefaultOptions()
	opts.RetentionDuration = 0
	tdb, err := tsdb.Open(tsdbPath, nil, nil, opts, nil)
	if err != nil {
		log.Fatalf("tsdb: %v", err)
	}

	wsManager := ws.NewManager()
	parserRegistry := registry.NewRegistry()
	metricsCache := metrics.NewRealtimeCache()
	uiBroadcaster := ws.NewAgentBroadcaster()

	routine := &routine.Routine{
		Store:       db,
		TSDB:        tdb,
		WS:          wsManager,
		Cache:       metricsCache,
		Broadcaster: uiBroadcaster,
	}
	go routine.Start(ctx)
	log.Println("Alert routine started in background...")

	// 3. Graceful shutdown
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		log.Println("Shutting down...")
		tdb.Close()
		db.Close()
		os.Exit(0)
	}()

	panelPath := getRoutePrefix("PANEL_PATH", "/")
	publicPath := getRoutePrefix("PUBLIC_PATH", "/dashboard")
	if publicPath == "/" {
		publicPath = "/dashboard"
	}

	// 4. Main Router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(CompressionMiddleware)

	// ─────────────────────────────────────────────────────────────
	// 5. API Route Definitions
	// ─────────────────────────────────────────────────────────────

	setupPanel := func(rt chi.Router) {
		rt.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rctx := context.WithValue(r.Context(), b_ctx.PanelPathKey, panelPath)
				next.ServeHTTP(w, r.WithContext(rctx))
			})
		})

		// Submit Endpoint
		rt.Post("/submit", agent.SubmitHandler(db, tdb, parserRegistry, metricsCache))
		rt.Get("/api/beszel/agent-connect", agent.BeszelWSHandler(db, tdb, wsManager, metricsCache))

		rt.Route("/api", func(api chi.Router) {
			api.Post("/login", auth.LoginHandler(db, db))
			api.Post("/logout", auth.LogoutHandler(db))

			api.Group(func(authApi chi.Router) {
				authApi.Get("/ws", requireAuth(db, ws.UIWebSocketHandler(uiBroadcaster)))
				authApi.Get("/agents", requireAuth(db, agent.ListAgentsHandler(db, metricsCache)))
				authApi.Post("/agent", requireAuth(db, agent.ProvisionAgentHandler(db, parserRegistry)))
				authApi.Get("/agent/install/{id}", requireAuth(db, agent.InstallAgentHandler(db)))
				authApi.Put("/agent", requireAuth(db, agent.RenameAgentHandler(db)))
				authApi.Delete("/agent", requireAuth(db, agent.RevokeAgentHandler(db, tdb)))
				authApi.Post("/agent/reset/ssh/{id}", requireAuth(db, agent.ResetAgentSSHKeyHandler(db, wsManager)))
				authApi.Post("/agent/reset/token/{id}", requireAuth(db, agent.ResetAgentTokenHandler(db, wsManager)))
				authApi.Get("/agent/ssh-key/{id}", requireAuth(db, agent.GetAgentSSHKeyHandler(db)))
				authApi.Get("/agents/management", requireAuth(db, agent.ListAgentsManagementHandler(db)))

			})

			api.Get("/metrics", requireAuth(db, metrics.MetricsQueryHandler(tdb, metricsCache)))

			api.Get("/dashboards", requireAuth(db, dashboard.ListDashboardsHandler(db)))
			api.Post("/dashboard", requireAuth(db, dashboard.CreateDashboardHandler(db)))
			api.Route("/dashboard/{id}", func(dashApi chi.Router) {
				dashApi.Get("/", requireAuth(db, dashboard.GetDashboardHandler(db)))
				dashApi.Put("/", requireAuth(db, dashboard.EditDashboardHandler(db)))
				dashApi.Delete("/", requireAuth(db, dashboard.DeleteDashboardHandler(db)))
			})

			api.Route("/user", func(userApi chi.Router) {
				userApi.Post("/password", requireAuth(db, auth.ChangePasswordHandler(db)))
				userApi.Get("/sessions", requireAuth(db, auth.ListSessionsHandler(db)))
				userApi.Delete("/session/{prefix}", requireAuth(db, auth.EjectSessionHandler(db)))
				userApi.Delete("/sessions/other", requireAuth(db, auth.EjectOtherSessionsHandler(db)))
			})

			api.Route("/alerts", func(alertApi chi.Router) {
				alertApi.Get("/", requireAuth(db, alert.ListAlertsHandler(db)))
				alertApi.Get("/history", requireAuth(db, alert.HistoryAlertHandler(db)))
				alertApi.Post("/", requireAuth(db, alert.CreateAlertHandler(db)))
				alertApi.Post("/test", requireAuth(db, alert.TestAlertHandler(db)))
				alertApi.Route("/{id}", func(idApi chi.Router) {
					idApi.Get("/", requireAuth(db, alert.GetAlertHandler(db)))
					idApi.Put("/", requireAuth(db, alert.EditAlertHandler(db)))
					idApi.Delete("/", requireAuth(db, alert.DeleteAlertHandler(db)))
				})
			})
		})

	}

	setupPublic := func(rt chi.Router) {
		rt.Route("/api/public", func(pubApi chi.Router) {
			pubApi.Get("/dashboard/{pub_id}", dashboard.PublicDashboardHandler(db))
			pubApi.Get("/metrics", metrics.PublicMetricsHandler(tdb, db, metricsCache))
			pubApi.Get("/ws/{id}", ws.PublicWebSocketHandler(db, uiBroadcaster))
		})
	}

	// ─────────────────────────────────────────────────────────────
	// 6. SPA Catch-all Handlers (Context Aware)
	// ─────────────────────────────────────────────────────────────
	// Admin Panel SPA
	panelSpa := spaHandler{
		staticPath:  "frontend-admin/out",
		indexPath:   "index.html",
		directIndex: true,
		basePath:    panelPath,
		envVars: map[string]string{
			"PANEL_PATH":  panelPath,
			"PUBLIC_PATH": publicPath,
		},
	}

	// Public Dashboard SPA
	publicSpa := spaHandler{
		staticPath:  "frontend-public/out",
		indexPath:   "index.html",
		directIndex: true,
		basePath:    publicPath,
		envVars: map[string]string{
			"PUBLIC_PATH": publicPath,
		},
	}

	// Set global 404 handler
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	})

	// mountContext safely attaches the API endpoints first, and ONLY attaches the
	// SPA catch-all at the very end of the sub-router to prevent overwriting the API.
	mountContext := func(basePath string, setup func(chi.Router), spa spaHandler) {
		if basePath == "/" {
			setup(r)
			r.Handle("/*", spa)
		} else {
			// 1. Force trailing slash: redirect /test to /test/
			r.Get(basePath, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, basePath+"/", http.StatusMovedPermanently)
			})

			// 2. Mount the sub-router for API and Assets
			r.Route(basePath+"/", func(sub chi.Router) {
				setup(sub)
				// The SPA handler itself (handles all non-API paths under this prefix)
				sub.Handle("/*", http.StripPrefix(basePath, spa))
			})
		}
	}

	if panelPath == publicPath {
		// If both run on identical paths, combine the APIs and use the Panel SPA fallback.
		// We also mount setupPublic under /dashboard so that when the dashboard is served
		// from its subpath (default config), its API calls stay prefix-consistent.
		mountContext(panelPath, func(rt chi.Router) {
			setupPanel(rt)
			setupPublic(rt)
			rt.Route("/dashboard", setupPublic)
		}, panelSpa)
	} else {
		// If paths differ, perfectly isolate them into their own sub-routers.
		mountContext(panelPath, setupPanel, panelSpa)

		// If publicPath is "/", we also mount it at "/dashboard" to match getPublicPath()
		if publicPath == "/" {
			mountContext(publicPath, func(rt chi.Router) {
				setupPublic(rt)
				rt.Route("/dashboard", setupPublic)
			}, publicSpa)
		} else {
			mountContext(publicPath, setupPublic, publicSpa)
		}
	}

	// ─────────────────────────────────────────────────────────────

	go startHeartbeatSweeper(db)
	go startSessionSweeper(db)

	log.Printf("CertainStats starting...")
	log.Printf(" → Internal Panel mapped to: http://localhost:8080%s", panelPath)
	log.Printf(" → Public Dashboards mapped to: http://localhost:8080%s", publicPath)

	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("Server: %v", err)
	}
}
