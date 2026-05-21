package main

import (
	"bytes"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

type spaHandler struct {
	staticPath   string // Used to identify which FS to fetch
	indexPath    string // e.g. "index.html"
	notFoundPath string // e.g. "404.html"
	envVars      map[string]string
	directIndex  bool   // when true, always serve indexPath for non-asset requests
	basePath     string // The mount path (e.g. /test)
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fsys := getFrontendFS(h.staticPath)

	// Clean path within the FS
	cleanPath := path.Clean(r.URL.Path)
	if cleanPath == "/" || cleanPath == "." {
		cleanPath = ""
	} else {
		cleanPath = strings.TrimPrefix(cleanPath, "/")
	}

	// 1. Check if the exact file exists and is not a directory
	var fileExists bool
	var isDir bool

	if cleanPath != "" {
		if stat, err := fs.Stat(fsys, cleanPath); err == nil {
			fileExists = true
			isDir = stat.IsDir()
		}
	}

	// Helper to serve file from fs.FS
	serveFSFile := func(w http.ResponseWriter, r *http.Request, filePath string, customHeaders map[string]string) bool {
		f, err := fsys.Open(filePath)
		if err != nil {
			return false
		}
		defer f.Close()

		stat, err := f.Stat()
		if err != nil {
			return false
		}
		if stat.IsDir() {
			return false
		}

		for k, v := range customHeaders {
			w.Header().Set(k, v)
		}

		// Since fs.File implements io.ReadSeeker (for both embed and OS filesystem),
		// we can type-assert it to support range requests, conditional requests, etc.
		if seeker, ok := f.(io.ReadSeeker); ok {
			http.ServeContent(w, r, path.Base(filePath), stat.ModTime(), seeker)
			return true
		}

		// Fallback read if not a seeker
		data, err := io.ReadAll(f)
		if err != nil {
			return false
		}
		http.ServeContent(w, r, path.Base(filePath), stat.ModTime(), bytes.NewReader(data))
		return true
	}

	if fileExists && !isDir {
		acceptEncoding := r.Header.Get("Accept-Encoding")

		// Determine original content type (mime type) to prevent browser rejection
		var mimeType string
		ext := path.Ext(cleanPath)
		switch strings.ToLower(ext) {
		case ".js":
			mimeType = "application/javascript"
		case ".css":
			mimeType = "text/css"
		case ".svg":
			mimeType = "image/svg+xml"
		case ".html":
			mimeType = "text/html; charset=utf-8"
		case ".json":
			mimeType = "application/json"
		case ".xml":
			mimeType = "application/xml"
		case ".txt":
			mimeType = "text/plain"
		}

		// Try serving pre-compressed Brotli (.br) first
		if strings.Contains(acceptEncoding, "br") {
			brPath := cleanPath + ".br"
			if brStat, brErr := fs.Stat(fsys, brPath); brErr == nil && !brStat.IsDir() {
				headers := map[string]string{
					"Content-Encoding": "br",
				}
				if mimeType != "" {
					headers["Content-Type"] = mimeType
				}
				if serveFSFile(w, r, brPath, headers) {
					return
				}
			}
		}

		// Try serving pre-compressed Gzip (.gz) next
		if strings.Contains(acceptEncoding, "gzip") {
			gzPath := cleanPath + ".gz"
			if gzStat, gzErr := fs.Stat(fsys, gzPath); gzErr == nil && !gzStat.IsDir() {
				headers := map[string]string{
					"Content-Encoding": "gzip",
				}
				if mimeType != "" {
					headers["Content-Type"] = mimeType
				}
				if serveFSFile(w, r, gzPath, headers) {
					return
				}
			}
		}

		// Fallback to uncompressed serving
		var headers map[string]string
		if mimeType != "" {
			headers = map[string]string{"Content-Type": mimeType}
		}
		if serveFSFile(w, r, cleanPath, headers) {
			return
		}
	}

	// 2. Resolve the HTML file to serve.
	var indexFilePath string
	var status = http.StatusOK

	if !h.directIndex && fileExists && isDir {
		pageIndex := path.Join(cleanPath, "index.html")
		if _, e2 := fs.Stat(fsys, pageIndex); e2 == nil {
			indexFilePath = pageIndex
		}
	}

	if indexFilePath == "" {
		if !fileExists && !h.directIndex {
			indexFilePath = h.notFoundPath
			status = http.StatusNotFound
		} else {
			indexFilePath = h.indexPath
		}
	}

	// 3. Serve the resolved file with environment injection.
	f, err := fsys.Open(indexFilePath)
	if err != nil {
		if status == http.StatusNotFound {
			http.NotFound(w, r)
			return
		}
		apiresponse.Error(w, http.StatusInternalServerError, "Internal Error")
		log.Debugf("Failed to load index.html: %s", err.Error())
		return
	}
	defer f.Close()

	indexBytes, readErr := io.ReadAll(f)
	if readErr != nil {
		apiresponse.Error(w, http.StatusInternalServerError, "Internal Error")
		log.Debugf("Failed to load index.html: %s", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	// Build the __APP_ENV__ JSON object.
	envJson := "{"
	for k, v := range h.envVars {
		envJson += fmt.Sprintf(`"%s": "%s",`, k, v)
	}
	envJson += "}"
	scriptContent := fmt.Sprintf("window.__APP_ENV__ = %s;", envJson)

	baseHref := h.basePath
	if baseHref != "/" && !strings.HasSuffix(baseHref, "/") {
		baseHref += "/"
	}
	if baseHref == "" {
		baseHref = "/"
	}

	injection := fmt.Sprintf(`<head><base href="%s"><script>%s</script>`, baseHref, scriptContent)
	w.Write(bytes.Replace(indexBytes, []byte("<head>"), []byte(injection), 1))
}
