package ltstats

import agentparser "certainstats/internal/agent_parser"

type LTstats struct {
	AgentInfo *agentparser.ParsedMetadata
	Metrics   []agentparser.Telemetry
}
