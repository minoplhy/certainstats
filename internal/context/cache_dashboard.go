package context

import "strings"

// InvalidateDashboard purges all cached JSON, HTML, and metric entries related to a dashboard slug.
func InvalidateDashboard(slug string) {
	if slug != "" {
		DashboardCache.Delete(slug)
		DashboardHTMLCache.Delete("html_dash_" + slug)
	}
	DashboardHTMLCache.Range(func(k, v any) bool {
		if keyStr, ok := k.(string); ok && strings.Contains(keyStr, slug) {
			DashboardHTMLCache.Delete(k)
		}
		return true
	})
	MetricsCache.Range(func(k, v any) bool {
		if keyStr, ok := k.(string); ok && strings.Contains(keyStr, slug) {
			MetricsCache.Delete(k)
		}
		return true
	})
}
