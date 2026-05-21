package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
		dataDir = "./data"
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

	cfg := LoadConfig()
	panelPath := cfg.PanelPath
	publicPath := cfg.PublicPath
	panelHost := cfg.PanelHost
	publicHost := cfg.PublicHost
	panelScheme := cfg.PanelScheme
	publicScheme := cfg.PublicScheme
	injectedPublicURL := cfg.InjectedPublicURL

	// 4. Router setups
	setupRouter := func(rt chi.Router) {
		rt.Use(middleware.RequestID)
		rt.Use(middleware.RealIP)
		rt.Use(middleware.Logger)
		rt.Use(middleware.Recoverer)
		rt.Use(CompressionMiddleware)
	}

	panelRouter := chi.NewRouter()
	setupRouter(panelRouter)

	publicRouter := chi.NewRouter()
	setupRouter(publicRouter)

	legacyRouter := chi.NewRouter()
	setupRouter(legacyRouter)

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
			"PUBLIC_URL":  injectedPublicURL,
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

	// Set global 404 handler for all sub-routers
	notFoundHandler := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}
	panelRouter.NotFound(notFoundHandler)
	publicRouter.NotFound(notFoundHandler)
	legacyRouter.NotFound(notFoundHandler)

	// mountContext attaches the API endpoints first, and then the SPA catch-all at the very end.
	mountContext := func(router chi.Router, basePath string, setup func(chi.Router), spa spaHandler) {
		if basePath == "/" {
			setup(router)
			router.Handle("/*", spa)
		} else {
			// 1. Force trailing slash: redirect /test to /test/
			router.Get(basePath, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, basePath+"/", http.StatusMovedPermanently)
			})

			// 2. Mount the sub-router for API and Assets
			router.Route(basePath+"/", func(sub chi.Router) {
				sub.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						// Ensure we preserve exact request path behavior
						next.ServeHTTP(w, r)
					})
				})
				setup(sub)
				// The SPA handler itself (handles all non-API paths under this prefix)
				sub.Handle("/*", spa)
			})
		}
	}

	// 7. Route and mount Virtual Hosts
	if panelHost != "" {
		mountContext(panelRouter, panelPath, setupPanel, panelSpa)
	}
	if publicHost != "" {
		if publicPath == "/" {
			mountContext(publicRouter, publicPath, func(rt chi.Router) {
				setupPublic(rt)
				rt.Route("/dashboard", setupPublic)
			}, publicSpa)
		} else {
			mountContext(publicRouter, publicPath, setupPublic, publicSpa)
		}
	}

	// 8. Legacy combined path-prefix routing (fallback)
	if panelPath == publicPath {
		mountContext(legacyRouter, panelPath, func(rt chi.Router) {
			setupPanel(rt)
			setupPublic(rt)
			rt.Route("/dashboard", setupPublic)
		}, panelSpa)
	} else {
		mountContext(legacyRouter, panelPath, setupPanel, panelSpa)

		if publicPath == "/" {
			mountContext(legacyRouter, publicPath, func(rt chi.Router) {
				setupPublic(rt)
				rt.Route("/dashboard", setupPublic)
			}, publicSpa)
		} else {
			mountContext(legacyRouter, publicPath, setupPublic, publicSpa)
		}
	}

	go startHeartbeatSweeper(db)
	go startSessionSweeper(db)

	log.Printf("CertainStats starting...")
	if panelHost != "" || publicHost != "" {
		log.Printf(" → Domain/Host-based Routing enabled:")
		if panelHost != "" {
			log.Printf("   - Admin Panel:        %s://%s%s", panelScheme, panelHost, panelPath)
		}
		if publicHost != "" {
			log.Printf("   - Public Dashboards:  %s://%s%s", publicScheme, publicHost, publicPath)
		}
	} else {
		log.Printf(" → Path-prefix Routing enabled (fallback):")
		log.Printf("   - Internal Panel mapped to: http://0.0.0.0:8080%s", panelPath)
		log.Printf("   - Public Dashboards mapped to: http://0.0.0.0:8080%s", publicPath)
	}

	masterHandler := HostRouter(panelHost, panelPath, publicHost, publicPath, panelRouter, publicRouter, legacyRouter)

	if err := http.ListenAndServe(":8080", masterHandler); err != nil {
		log.Fatalf("Server: %v", err)
	}
}
