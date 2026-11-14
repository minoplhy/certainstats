package collector

import (
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/security"
	"certainstats/internal/store"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// MaxConnections caps live collector connections across all users.
const MaxConnections = 256

// Service is the collector parent: registered types, live connections and the API.
type Service struct {
	Store  store.CollectorStore
	Agents store.AgentStore
	// Limits throttles connection attempts per IP; nil uses security.CollectorLimits.
	Limits *security.Limiter
	types  map[string]Type
	conns  *registry
}

func NewService(collectors store.CollectorStore, agents store.AgentStore) *Service {
	return &Service{Store: collectors, Agents: agents, types: map[string]Type{}, conns: newRegistry(MaxConnections)}
}

// Register adds a type. Names and prefixes must be unique and the prefix must end in "_".
func (s *Service) Register(t Type) error {
	if _, dup := s.types[t.Name()]; dup {
		return fmt.Errorf("collector type %q registered twice", t.Name())
	}
	p := t.Prefix()
	if len(p) < 2 || p[len(p)-1] != '_' {
		return fmt.Errorf("collector type %q: prefix %q must end in _", t.Name(), p)
	}
	for _, other := range s.types {
		if other.Prefix() == p {
			return fmt.Errorf("collector types %q and %q share prefix %q", other.Name(), t.Name(), p)
		}
	}
	s.types[t.Name()] = t
	return nil
}

// Types returns the registered types in name order.
func (s *Service) Types() []Type {
	list := make([]Type, 0, len(s.types))
	for _, t := range s.types {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name() < list[j].Name() })
	return list
}

func (s *Service) typeByPrefix(prefix string) Type {
	for _, t := range s.types {
		if t.Prefix() == prefix {
			return t
		}
	}
	return nil
}

// Conn returns the collector's live connection, or nil.
func (s *Service) Conn(collectorID string) *Conn { return s.conns.get(collectorID) }

// Disconnect closes a collector's live connection, if any.
func (s *Service) Disconnect(collectorID string) { s.conns.close(collectorID) }

// Close ends every live connection, at shutdown.
func (s *Service) Close() { s.conns.closeAll() }

// HashToken is how tokens are stored; the raw token is shown once and never kept.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() string { return "csc_" + agentdata.GenerateRandomString(48) }

func newID() string { return "col_" + agentdata.GenerateRandomString(24) }
