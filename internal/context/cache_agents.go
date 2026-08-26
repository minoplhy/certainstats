package context

import "strings"

// InvalidateAgent purges an agent across DeviceCache, PublicAgentCache, and MetricsCache.
func InvalidateAgent(agentID string) {
	DeviceCache.Range(func(key, value any) bool {
		if keyStr, ok := key.(string); ok && strings.Contains(keyStr, agentID) {
			DeviceCache.Delete(key)
		}
		return true
	})
	PublicAgentCache.Range(func(key, value any) bool {
		if v, ok := value.(*PublicAgentCacheEntry); ok && v.Agent.RealAgentID == agentID {
			PublicAgentCache.Delete(key)
		}
		return true
	})
	MetricsCache.Range(func(key, value any) bool {
		if keyStr, ok := key.(string); ok && strings.Contains(keyStr, agentID) {
			MetricsCache.Delete(key)
		}
		return true
	})
}
