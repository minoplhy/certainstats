package context

// Permission changes purge all dynamic namespaces. Configuration versions also
// prevent an in-flight build from becoming reachable after invalidation.
func InvalidateDashboard(id string) {
	for _, c := range []*ResponseCache{&DashboardCache, &DashboardHTMLCache, &MetricsCache} {
		c.Range(func(k, v any) bool { c.Delete(k); return true })
	}
	PublicAgentCache.Range(func(k, v any) bool { PublicAgentCache.Delete(k); return true })
	DashboardRevoked(id)
}

// Registered by the browser broadcaster at startup; storage mutations invoke it
// after commit, so revoked sockets close without waiting for the next pulse.
var DashboardRevoked = func(string) {}
