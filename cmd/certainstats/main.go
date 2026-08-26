package main

import (
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/web"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"certainstats/internal/agent"
	"certainstats/internal/agent_parser/registry"
	"certainstats/internal/alert"
	"certainstats/internal/auth"
	"certainstats/internal/compress"
	b_ctx "certainstats/internal/context"
	"certainstats/internal/dashboard"
	"certainstats/internal/lifecycle"
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

		var exitCode int
		select {
		case sig := <-stop:
			log.Printf("Received system signal %v. Shutting down cleanly...", sig)
			exitCode = 0
		case code := <-lifecycle.ShutdownChan:
			log.Printf("Application requested clean graceful restart (exit code %d)...", code)
			exitCode = code
		}

		log.Println("Closing TSDB and SQLite database handles gracefully...")
		tdb.Close()
		db.Close()
		os.Exit(exitCode)
	}()

	cfg := LoadConfig()
	panelPath := cfg.PanelPath
	publicPath := cfg.PublicPath
	panelHost := cfg.PanelHost
	publicHost := cfg.PublicHost
	panelScheme := cfg.PanelScheme
	publicScheme := cfg.PublicScheme

	// 3.5 First-Time Setup initialization check
	isZero, err := db.IsUserZero(ctx)
	if err != nil {
		log.Fatalf("database user check: %v", err)
	}

	if isZero {
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			log.Fatalf("failed to generate setup token: %v", err)
		}
		setupTok := hex.EncodeToString(tokenBytes)
		auth.SetSetupToken(setupTok)

		displayHost := cfg.PanelHost
		if displayHost == "" {
			displayHost = cfg.Host
			if displayHost == "" {
				displayHost = "0.0.0.0"
			}
			displayHost = displayHost + ":" + cfg.Port
		}

		scheme := cfg.PanelScheme
		if scheme == "" {
			scheme = "http"
		}

		pPath := cfg.PanelPath
		if pPath == "/" {
			pPath = ""
		}

		setupURL := fmt.Sprintf("%s://%s%s/first-time-setup?token=%s", scheme, displayHost, pPath, setupTok)

		log.Println("====================================================================")
		log.Printf("  !!SETUP REQUIRED!!")
		log.Printf("  Please visit: %s", setupURL)
		log.Printf("  or enter the secure 32-byte setup token from your logs:")
		log.Printf("  Token: %s", setupTok)
		log.Println("====================================================================")
	}

	// 3.6 Web Static Pipeline & Template Renderer Initialization
	if err := web.InitStatic(); err != nil {
		log.Fatalf("static asset pipeline: %v", err)
	}
	renderer, err := web.NewRenderer()
	if err != nil {
		log.Fatalf("template renderer: %v", err)
	}

	staticPath := "/static"
	if panelPath != "/" {
		staticPath = panelPath + "/static"
	}

	webHandler := &web.WebHandler{
		Renderer:   renderer,
		Store:      db,
		Cache:      metricsCache,
		PanelPath:  panelPath,
		PublicPath: publicPath,
		StaticPath: staticPath,
	}

	// 4. Router setups
	setupRouter := func(rt chi.Router) {
		rt.Use(middleware.RequestID)
		rt.Use(middleware.RealIP)
		rt.Use(middleware.Logger)
		rt.Use(middleware.Recoverer)
		rt.Use(compress.CompressionMiddleware)
	}

	panelRouter := chi.NewRouter()
	setupRouter(panelRouter)

	publicRouter := chi.NewRouter()
	setupRouter(publicRouter)

	legacyRouter := chi.NewRouter()
	setupRouter(legacyRouter)

	// ─────────────────────────────────────────────────────────────
	// 5. Route Definitions (Web 1.0 HTML Pages + JSON APIs)
	// ─────────────────────────────────────────────────────────────

	setupPanel := func(rt chi.Router) {
		rt.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rctx := context.WithValue(r.Context(), b_ctx.PanelPathKey, panelPath)
				next.ServeHTTP(w, r.WithContext(rctx))
			})
		})

		// Static assets
		web.ServeStatic(rt, "/static")

		// Agent Submission Endpoints
		rt.Post("/submit", agent.SubmitHandler(db, tdb, parserRegistry, metricsCache))
		rt.Get("/api/beszel/agent-connect", agent.BeszelWSHandler(db, tdb, wsManager, metricsCache))

		// Web 1.0 HTML Routes
		rt.Get("/first-time-setup", webHandler.SetupHandler)
		rt.Post("/first-time-setup", webHandler.SetupHandler)
		rt.Get("/login", webHandler.LoginHandler)
		rt.Post("/login", webHandler.LoginHandler)
		rt.Post("/logout", webHandler.LogoutHandler)

		rt.Get("/", webHandler.RequireAuthWeb(webHandler.AgentsListHandler))
		rt.Get("/{id}", webHandler.RequireAuthWeb(webHandler.AgentsListHandler))

		rt.Get("/agents/management", webHandler.RequireAuthWeb(webHandler.AgentManagementHandler))
		rt.Post("/agent/provision", webHandler.RequireAuthWeb(webHandler.AgentProvisionHandler))
		rt.Post("/agent/reset/token", webHandler.RequireAuthWeb(webHandler.AgentResetTokenHandler))
		rt.Post("/agent/reset/ssh", webHandler.RequireAuthWeb(webHandler.AgentResetSSHHandler))
		rt.Post("/agent/delete", webHandler.RequireAuthWeb(webHandler.AgentDeleteHandler))

		rt.Get("/dashboards", webHandler.RequireAuthWeb(webHandler.DashboardsListHandler))
		rt.Get("/dashboard/create", webHandler.RequireAuthWeb(webHandler.DashboardCreatePageHandler))
		rt.Post("/dashboard/create", webHandler.RequireAuthWeb(webHandler.DashboardCreateHandler))
		rt.Get("/dashboard/{id}", webHandler.RequireAuthWeb(webHandler.DashboardEditHandler))
		rt.Post("/dashboard/{id}", webHandler.RequireAuthWeb(webHandler.DashboardUpdateHandler))
		rt.Delete("/dashboard/{id}", webHandler.RequireAuthWeb(webHandler.DashboardDeleteHandler))

		rt.Get("/alerts", webHandler.RequireAuthWeb(webHandler.AlertsListHandler))
		rt.Post("/alerts/create", webHandler.RequireAuthWeb(webHandler.AlertCreateHandler))
		rt.Post("/alerts/edit", webHandler.RequireAuthWeb(webHandler.AlertUpdateHandler))
		rt.Post("/alerts/delete", webHandler.RequireAuthWeb(webHandler.AlertDeleteHandler))
		rt.Post("/alerts/targets/create", webHandler.RequireAuthWeb(webHandler.TargetCreateHandler))
		rt.Post("/alerts/targets/edit", webHandler.RequireAuthWeb(webHandler.TargetUpdateHandler))
		rt.Post("/alerts/targets/delete", webHandler.RequireAuthWeb(webHandler.TargetDeleteHandler))

		rt.Get("/settings", webHandler.RequireAuthWeb(webHandler.SettingsHandler))
		rt.Post("/settings/password", webHandler.RequireAuthWeb(webHandler.PasswordChangeHandler))
		rt.Post("/settings/sessions/eject", webHandler.RequireAuthWeb(webHandler.SessionEjectHandler))
		rt.Post("/settings/sessions/eject-other", webHandler.RequireAuthWeb(webHandler.SessionEjectOtherHandler))

		// SPA route for /{agent_id}
		rt.Get("/{id}", webHandler.RequireAuthWeb(webHandler.AgentsListHandler))

		// REST JSON API Routes
		rt.Route("/api", func(api chi.Router) {
			api.Post("/login", auth.LoginHandler(db, db))
			api.Post("/logout", auth.LogoutHandler(db))

			if isZero {
				api.Get("/first-time-setup/status", auth.GetSetupStatusHandler(db))
				api.Get("/first-time-setup/check", auth.CheckSetupHandler())
				api.Post("/first-time-setup", auth.RegisterFirstUserHandler(db))
				api.Post("/first-time-setup/restart", auth.RestartServerHandler())
			}

			api.Group(func(authApi chi.Router) {
				authApi.Get("/ws", requireAuth(db, ws.UIWebSocketHandler(uiBroadcaster)))
				authApi.Get("/agents", requireAuth(db, agent.ListAgentsHandler(db, metricsCache)))
				authApi.Post("/agent", requireAuth(db, agent.ProvisionAgentHandler(db, parserRegistry)))
				authApi.Get("/agent/install/{id}", requireAuth(db, agent.InstallAgentHandler(db)))
				authApi.Get("/agent/uninstall/{id}", requireAuth(db, agent.UninstallAgentHandler(db)))
				authApi.Put("/agent", requireAuth(db, agent.RenameAgentHandler(db)))
				authApi.Delete("/agent", requireAuth(db, agent.RevokeAgentHandler(db, tdb, metricsCache)))
				authApi.Post("/agent/reset/ssh/{id}", requireAuth(db, agent.ResetAgentSSHKeyHandler(db, wsManager)))
				authApi.Post("/agent/reset/token/{id}", requireAuth(db, agent.ResetAgentTokenHandler(db, wsManager)))
				authApi.Get("/agent/ssh-key/{id}", requireAuth(db, agent.GetAgentSSHKeyHandler(db)))
				authApi.Get("/agents/management", requireAuth(db, agent.ListAgentsManagementHandler(db)))
			})

			api.Get("/metrics", requireAuth(db, metrics.MetricsQueryHandler(db, tdb, metricsCache)))

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
				alertApi.Post("/history/retry/{id}", requireAuth(db, alert.RetryAlertHandler(db)))
				alertApi.Post("/", requireAuth(db, alert.CreateAlertHandler(db)))
				alertApi.Post("/test", requireAuth(db, alert.TestAlertHandler(db)))

				alertApi.Route("/targets", func(targetApi chi.Router) {
					targetApi.Get("/", requireAuth(db, alert.ListTargetsHandler(db)))
					targetApi.Post("/", requireAuth(db, alert.CreateTargetHandler(db)))
					targetApi.Post("/test", requireAuth(db, alert.TestTargetHandler(db)))
					targetApi.Route("/{id}", func(idApi chi.Router) {
						idApi.Get("/", requireAuth(db, alert.GetTargetHandler(db)))
						idApi.Put("/", requireAuth(db, alert.EditTargetHandler(db)))
						idApi.Delete("/", requireAuth(db, alert.DeleteTargetHandler(db)))
					})
				})

				alertApi.Route("/{id}", func(idApi chi.Router) {
					idApi.Get("/", requireAuth(db, alert.GetAlertHandler(db)))
					idApi.Put("/", requireAuth(db, alert.EditAlertHandler(db)))
					idApi.Delete("/", requireAuth(db, alert.DeleteAlertHandler(db)))
				})
			})
		})
	}

	setupPublic := func(rt chi.Router) {
		web.ServeStatic(rt, "/static")

		// Register API routes BEFORE wildcard slug routes so they are never intercepted
		rt.Route("/api/public", func(pubApi chi.Router) {
			pubApi.Get("/dashboard/{pub_id}", dashboard.PublicDashboardHandler(db))
			pubApi.Get("/metrics", metrics.PublicMetricsHandler(tdb, db, metricsCache))
			pubApi.Get("/ws/{id}", ws.PublicWebSocketHandler(db, uiBroadcaster))
		})

		rt.Get("/dashboard/{slug}", webHandler.PublicDashboardHandler)
		rt.Get("/dashboard/{slug}/{pub_id}", webHandler.PublicDashboardHandler)
		rt.Get("/{slug}", webHandler.PublicDashboardHandler)
		rt.Get("/{slug}/{pub_id}", webHandler.PublicDashboardHandler)
	}

	notFoundHandler := func(w http.ResponseWriter, r *http.Request) {
		apiresponse.Error(w, http.StatusNotFound, "Not Found")
	}
	panelRouter.NotFound(notFoundHandler)
	publicRouter.NotFound(notFoundHandler)
	legacyRouter.NotFound(notFoundHandler)

	mountContext := func(router chi.Router, basePath string, setup func(chi.Router)) {
		log.Debugf("[Mount] setting up router mount for basePath=%q", basePath)
		if basePath == "/" {
			setup(router)
			log.Debugf("[Mount] registered root handlers for basePath=%q", basePath)
		} else {
			router.Get(basePath, func(w http.ResponseWriter, r *http.Request) {
				log.Debugf("[Mount] redirecting un-slashed request %q to %s/", r.URL.Path, basePath)
				http.Redirect(w, r, basePath+"/", http.StatusMovedPermanently)
			})

			router.Route(basePath+"/", func(sub chi.Router) {
				setup(sub)
				log.Debugf("[Mount] registered subpath handlers under %s/", basePath)
			})
		}
	}

	// 7. Route and mount Virtual Hosts
	if panelHost != "" {
		mountContext(panelRouter, panelPath, setupPanel)
	}
	if publicHost != "" {
		if publicPath == "/" {
			mountContext(publicRouter, publicPath, func(rt chi.Router) {
				setupPublic(rt)
				rt.Route("/dashboard", setupPublic)
			})
		} else {
			mountContext(publicRouter, publicPath, setupPublic)
		}
	}

	// 8. Legacy combined path-prefix routing (fallback)
	if panelPath == publicPath {
		mountContext(legacyRouter, panelPath, func(rt chi.Router) {
			setupPanel(rt)
			setupPublic(rt)
			rt.Route("/dashboard", setupPublic)
		})
	} else {
		mountContext(legacyRouter, panelPath, setupPanel)

		if publicPath == "/" {
			mountContext(legacyRouter, publicPath, func(rt chi.Router) {
				setupPublic(rt)
				rt.Route("/dashboard", setupPublic)
			})
		} else {
			mountContext(legacyRouter, publicPath, setupPublic)
		}
	}

	go startHeartbeatSweeper(db, metricsCache)
	go startSessionSweeper(db)

	log.Printf("CertainStats starting (Web 1.0 Templates Mode)...")
	displayHost := cfg.Host
	if displayHost == "" {
		displayHost = "0.0.0.0"
	}
	displayAddr := displayHost + ":" + cfg.Port

	if panelHost != "" {
		log.Printf("   - Admin Panel:        %s://%s%s", panelScheme, panelHost, panelPath)
	} else {
		log.Printf("   - Admin Panel:        http://%s%s", displayAddr, panelPath)
	}
	if publicHost != "" {
		log.Printf("   - Public Dashboards:  %s://%s%s", publicScheme, publicHost, publicPath)
	} else {
		log.Printf("   - Public Dashboards:  http://%s%s", displayAddr, publicPath)
	}

	masterHandler := HostRouter(panelHost, panelPath, publicHost, publicPath, panelRouter, publicRouter, legacyRouter)

	log.Printf("Starting server on %s", displayAddr)
	if err := http.ListenAndServe(cfg.Host+":"+cfg.Port, masterHandler); err != nil {
		log.Fatalf("Server: %v", err)
	}
}
