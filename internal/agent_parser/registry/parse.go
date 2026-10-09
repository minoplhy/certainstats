package registry

import (
	beszel "certainstats/internal/agent_parser/Beszel"
	hetrixtools "certainstats/internal/agent_parser/HetrixTools"
	ltstats "certainstats/internal/agent_parser/LTstats"
	"certainstats/internal/agentmeta"
	nm "certainstats/internal/networkmonitor"
	"errors"

	agentparser "certainstats/internal/agent_parser"
)

type Registry struct {
	parsers map[string]agentparser.AgentParser
}

func NewRegistry() *Registry {
	r := &Registry{
		parsers: make(map[string]agentparser.AgentParser),
	}

	// Register all available parsers
	r.Register(&ltstats.LTstats{})
	r.Register(&hetrixtools.HTStats{})
	r.Register(&beszel.BeszelStats{})

	return r
}

func (r *Registry) IsSupported(agentType string) bool {
	_, exists := r.parsers[agentType]
	return exists
}

func (r *Registry) Register(p agentparser.AgentParser) {
	r.parsers[p.AgentType()] = p
}

func (r *Registry) ParsePayload(agentType string, rawPayload []byte) (*agentparser.ParsedData, error) {
	parser, exists := r.parsers[agentType]
	if !exists {
		return nil, errors.New("unsupported agent type: " + agentType)
	}

	return parser.Parse(rawPayload)
}

func (r *Registry) ParseToken(agentType string, rawPayload []byte) (string, error) {
	parser, exists := r.parsers[agentType]
	if !exists {
		return "", errors.New("unsupported agent type: " + agentType)
	}
	return parser.ParseToken(rawPayload)
}

func (r *Registry) Detect(rawPayload []byte) (string, string, error) {
	// We try to detect the agent type by attempting to parse the token.
	// Since LTStats is binary and very lenient, we try other parsers first.

	// Try HetrixTools first as it has more validation (base64 + JSON)
	if token, err := r.ParseToken("hetrixtools", rawPayload); err == nil && token != "" {
		return "hetrixtools", token, nil
	}

	// Fallback to LTStats
	if token, err := r.ParseToken("ltstats", rawPayload); err == nil && token != "" {
		return "ltstats", token, nil
	}

	return "", "", errors.New("could not identify agent type")
}

// CapabilityResolver is implemented by providers that support network monitors.
type CapabilityResolver interface {
	ResolveCapabilities(r agentmeta.Runtime) agentmeta.Capabilities
}

// Capabilities are resolved by the registered provider, never by global version comparisons.
func (r *Registry) Capabilities(agentType string, runtime agentmeta.Runtime) agentmeta.Capabilities {
	if resolver, ok := r.parsers[agentType].(CapabilityResolver); ok {
		return resolver.ResolveCapabilities(runtime)
	}
	return agentmeta.Unsupported("unsupported_provider", "This agent integration does not support network monitors")
}

// NetworkAdapter returns the provider's monitor configuration adapter, if any.
func (r *Registry) NetworkAdapter(agentType string) (nm.Adapter, bool) {
	adapter, ok := r.parsers[agentType].(nm.Adapter)
	return adapter, ok
}
