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

// ServeStatic mounts in-memory static asset serving onto a Chi router.
// It dynamically calculates the route pattern prefix so it works on any subpath, host, or custom prefix.
func ServeStatic(r chi.Router, path string) {
	if strings.Contains(path, ":") || strings.Contains(path, "*") {
		panic("ServeStatic does not permit URL parameters")
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

			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			entry.Serve(w, r, contentType, http.StatusOK)
			return
		}

		http.NotFound(w, r)
	})
}
