package web

import (
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/minify"
	"certainstats/web"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"math"
	"net/http"
	"strings"
	"time"
)

type PageData struct {
	Title         string
	PanelPath     string
	PublicPath    string
	StaticPath    string
	ActiveNav     string
	Authenticated bool
	FlashSuccess  string
	FlashError    string
	Year          int
	Data          any
	StartTime     time.Time
}

// RenderTime returns the elapsed time since StartTime, formatted with µs or ms precision.
func (d PageData) RenderTime() string {
	if d.StartTime.IsZero() {
		return "0.00ms"
	}
	dur := time.Since(d.StartTime)
	if dur < time.Millisecond {
		return fmt.Sprintf("%dµs", dur.Microseconds())
	}
	return fmt.Sprintf("%.2fms", float64(dur.Microseconds())/1000.0)
}

type TemplateRenderer struct {
	templates map[string]*template.Template
}

func toUint64(val any) uint64 {
	switch v := val.(type) {
	case uint64:
		return v
	case *uint64:
		if v != nil {
			return *v
		}
	case uint32:
		return uint64(v)
	case *uint32:
		if v != nil {
			return uint64(*v)
		}
	case int:
		return uint64(v)
	case int64:
		return uint64(v)
	case float64:
		return uint64(v)
	}
	return 0
}

func NewRenderer() (*TemplateRenderer, error) {
	funcMap := template.FuncMap{
		"asset":     minify.AssetPath,
		"integrity": minify.AssetIntegrity,
		"isOnline": func(val any) bool {
			if val == nil {
				return false
			}
			switch v := val.(type) {
			case bool:
				return v
			case *bool:
				return v != nil && *v
			}
			return false
		},
		"formatBytes": func(val any) string {
			bytes := toUint64(val)
			if bytes == 0 {
				return "0 B"
			}
			units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
			i := int(math.Floor(math.Log(float64(bytes)) / math.Log(1024)))
			if i >= len(units) {
				i = len(units) - 1
			}
			valNum := float64(bytes) / math.Pow(1024, float64(i))
			return fmt.Sprintf("%.1f %s", valNum, units[i])
		},
		"formatUptime": func(val any) string {
			seconds := toUint64(val)
			if seconds == 0 {
				return "0m"
			}
			d := time.Duration(seconds) * time.Second
			days := int(d.Hours()) / 24
			hours := int(d.Hours()) % 24
			minutes := int(d.Minutes()) % 60

			if days > 0 {
				return fmt.Sprintf("%dd %dh", days, hours)
			}
			if hours > 0 {
				return fmt.Sprintf("%dh %dm", hours, minutes)
			}
			return fmt.Sprintf("%dm", minutes)
		},
		"percentage": func(usedVal, totalVal any) float64 {
			used := toUint64(usedVal)
			total := toUint64(totalVal)
			if total == 0 {
				return 0
			}
			pct := (float64(used) / float64(total)) * 100.0
			if pct > 100 {
				return 100
			}
			return pct
		},
		"timeAgo": func(t time.Time) string {
			if t.IsZero() {
				return "Never"
			}
			diff := time.Since(t)
			if diff < time.Minute {
				return "Just now"
			}
			if diff < time.Hour {
				return fmt.Sprintf("%dm ago", int(diff.Minutes()))
			}
			if diff < 24*time.Hour {
				return fmt.Sprintf("%dh ago", int(diff.Hours()))
			}
			return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
		},
		"hasFeature": func(rules any, feature string) bool {
			if r, ok := rules.(accessrules.AccessRule); ok {
				for _, f := range r.AllowedFeatures {
					if f == feature {
						return true
					}
				}
			}
			return false
		},
		"hasMetric": func(rules any, metric string) bool {
			if r, ok := rules.(accessrules.AccessRule); ok {
				for _, m := range r.AllowedMetrics {
					if m == metric {
						return true
					}
				}
			}
			return false
		},
	}

	tmplFS := web.TemplatesFS()
	pages := []string{
		"setup.html",
		"login.html",
		"agents_list.html",
		"agent_management.html",
		"dashboards_list.html",
		"dashboard_edit.html",
		"public_dashboard.html",
		"alerts_list.html",
		"settings.html",
	}

	var partialFiles []string
	_ = fs.WalkDir(tmplFS, "partials", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".html") {
			partialFiles = append(partialFiles, path)
		}
		return nil
	})

	tmplMap := make(map[string]*template.Template)

	for _, page := range pages {
		baseLayout := "layout/base.html"
		if page == "public_dashboard.html" {
			baseLayout = "layout/public_base.html"
		}

		patterns := append([]string{baseLayout, page}, partialFiles...)
		t, err := template.New(page).Funcs(funcMap).ParseFS(tmplFS, patterns...)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", page, err)
		}
		tmplMap[page] = t
	}

	return &TemplateRenderer{templates: tmplMap}, nil
}

func (r *TemplateRenderer) Render(w io.Writer, page string, data any) error {
	tmpl, ok := r.templates[page]
	if !ok {
		return fmt.Errorf("template %s not found", page)
	}

	execName := "base"
	if page == "public_dashboard.html" {
		execName = "public_base"
	}

	switch d := data.(type) {
	case *PageData:
		if d.StartTime.IsZero() {
			d.StartTime = time.Now()
		}
		if m, ok := d.Data.(map[string]any); ok && m != nil {
			if _, ok := m["StaticPath"]; !ok && d.StaticPath != "" {
				m["StaticPath"] = d.StaticPath
			}
			if _, ok := m["PanelPath"]; !ok && d.PanelPath != "" {
				m["PanelPath"] = d.PanelPath
			}
			if _, ok := m["PublicPath"]; !ok && d.PublicPath != "" {
				m["PublicPath"] = d.PublicPath
			}
		}
	case PageData:
		if d.StartTime.IsZero() {
			d.StartTime = time.Now()
		}
		if m, ok := d.Data.(map[string]any); ok && m != nil {
			if _, ok := m["StaticPath"]; !ok && d.StaticPath != "" {
				m["StaticPath"] = d.StaticPath
			}
			if _, ok := m["PanelPath"]; !ok && d.PanelPath != "" {
				m["PanelPath"] = d.PanelPath
			}
			if _, ok := m["PublicPath"]; !ok && d.PublicPath != "" {
				m["PublicPath"] = d.PublicPath
			}
		}
		data = d
	}

	return tmpl.ExecuteTemplate(w, execName, data)
}

func (r *TemplateRenderer) RenderHTTP(w http.ResponseWriter, status int, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	if err := r.Render(w, page, data); err != nil {
		http.Error(w, fmt.Sprintf("Template Error: %v", err), http.StatusInternalServerError)
	}
}
