package sqlite

import (
	"certainstats/internal/agentmeta"
	alert "certainstats/internal/base/alert"
	"context"
)

// incidentCapabilities disables monitoring and retry actions on a network
// incident whose agent no longer supports the monitor's configuration.
func (s *Store) incidentCapabilities(ctx context.Context, q queryRow, h *alert.AlertHistory) error {
	if h.MonitorID == "" || !h.MonitoringAvailable {
		return nil
	}
	agent, err := s.loadMonitorAgent(ctx, q, h.AgentID, h.UserID)
	if err != nil {
		return err
	}
	caps := agent.capabilities
	if !caps.SupportsConfig(h.Monitor.Protocol, h.Monitor.Server) || !caps.Supports(agentmeta.FeatureHourlyLoss) {
		h.MonitoringAvailable = false
		h.RetryAvailable = false
	}
	return nil
}
