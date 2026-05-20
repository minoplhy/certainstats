package hetrixtools

import agentparser "certainstats/internal/agent_parser"

type HTStats struct {
	AgentInfo *agentparser.ParsedMetadata
	Metrics   []agentparser.Telemetry
}
