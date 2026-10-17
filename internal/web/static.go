package web

import (
	ctx "certainstats/internal/context"
	"certainstats/internal/minify"
	"certainstats/web"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

var (
	staticCacheOnce sync.Once
	staticCacheErr  error
)

// InitStatic eagerly loads, minifies, fingerprints, and pre-compresses all static files into ctx.StaticCache in RAM.
// It is idempotent and safe to call multiple times.
func InitStatic() error {
	staticCacheOnce.Do(func() {
		staticFS := web.StaticFS()
		staticCacheErr = fs.WalkDir(staticFS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			data, err := fs.ReadFile(staticFS, path)
			if err != nil {
				return err
			}

			cleanPath := strings.TrimPrefix(filepath.ToSlash(path), "/")
			fpPath, processed, err := minify.Process(cleanPath, data)
			if err != nil {
				return err
			}

			entry := ctx.NewCacheEntry(processed, ctx.LongCacheTTL)
			ctx.StaticCache.Store(fpPath, entry)
			if fpPath != cleanPath {
				ctx.StaticCache.Store(cleanPath, entry)
			}
			return nil
		})
	})
	return staticCacheErr
}

// StaticArea is a top-level folder of web/static that a router may serve, e.g. "shared" or "admin".
// When Auth is set, the area is only served to requests it accepts; others get a 404.
type StaticArea struct {
	Prefix string
	Auth   func(*http.Request) bool
}

// PanelStaticAreas are served by the admin panel: shared and auth assets to anyone, admin assets
// only to requests hasSession accepts.
func PanelStaticAreas(hasSession func(*http.Request) bool) []StaticArea {
	return []StaticArea{{Prefix: "shared"}, {Prefix: "auth"}, {Prefix: "admin", Auth: hasSession}}
}

// PublicStaticAreas are served by the public dashboards; admin and auth assets never are.
func PublicStaticAreas() []StaticArea {
	return []StaticArea{{Prefix: "shared"}, {Prefix: "public"}}
}

// area returns the area relPath belongs to, if it is one of areas.
func area(relPath string, areas []StaticArea) (StaticArea, bool) {
	for _, a := range areas {
		if strings.HasPrefix(relPath, a.Prefix+"/") {
			return a, true
		}
	}
	return StaticArea{}, false
}

// ServeStatic mounts in-memory static asset serving onto a Chi router, limited to the given areas.
// It dynamically calculates the route pattern prefix so it works on any subpath, host, or custom prefix.
func ServeStatic(r chi.Router, path string, areas ...StaticArea) {
	if strings.Contains(path, ":") || strings.Contains(path, "*") {
		panic("ServeStatic does not permit URL parameters")
	}
	// Serving nothing by default keeps a forgotten argument from exposing every area.
	if len(areas) == 0 {
		panic("ServeStatic needs at least one static area")
	}

	_ = InitStatic()

	workPath := path
	if workPath != "/" && !strings.HasSuffix(workPath, "/") {
		workPath += "/"
	}
	pattern := workPath + "*"

	r.Get(pattern, func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		if pathPrefix == "" {
			pathPrefix = "/"
		}

		relPath := strings.TrimPrefix(r.URL.Path, pathPrefix)
		relPath = strings.TrimPrefix(relPath, "/")

		served, ok := area(relPath, areas)
		if !ok || strings.Contains(relPath, "..") {
			http.NotFound(w, r)
			return
		}
		cacheControl := "public, max-age=31536000, immutable"
		if served.Auth != nil {
			if !served.Auth(r) {
				w.Header().Set("Cache-Control", "private, no-store")
				http.NotFound(w, r)
				return
			}
			cacheControl = "private, max-age=31536000, immutable"
		}

		if entry, hit := ctx.GetCacheEntry(&ctx.StaticCache, relPath); hit {
			ext := filepath.Ext(relPath)
			contentType := mime.TypeByExtension(ext)
			if contentType == "" {
				switch ext {
				case ".css":
					contentType = "text/css; charset=utf-8"
				case ".js":
					contentType = "application/javascript; charset=utf-8"
				case ".svg":
					contentType = "image/svg+xml"
				case ".png":
					contentType = "image/png"
				case ".jpg", ".jpeg":
					contentType = "image/jpeg"
				case ".webp":
					contentType = "image/webp"
				case ".woff2":
					contentType = "font/woff2"
				case ".woff":
					contentType = "font/woff"
				case ".ttf":
					contentType = "font/ttf"
				default:
					contentType = "application/octet-stream"
				}
			}

			w.Header().Set("Cache-Control", cacheControl)
			entry.Serve(w, r, contentType, http.StatusOK)
			return
		}

		http.NotFound(w, r)
	})
}
