package store

import (
	b "certainstats/internal/base/agent"
	c "certainstats/internal/base/alert"
	a "certainstats/internal/base/response"
	"certainstats/internal/dashboard/accessrules"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

/*
These Struct are to represents output from SQL Query
*/

type User struct {
	UserID       string
	Username     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

type Session struct {
	Token           string
	UserID          string
	ExpiresAt       time.Time
	CreatedAt       time.Time
	LastConnectedAt time.Time
	IPAddress       string
	UserAgent       string
}

// HashTokenPrefix returns a unique, safe prefix/hash of the session token.
func HashTokenPrefix(token string) string {
	h := sha256.New()
	h.Write([]byte(token))
	hashStr := hex.EncodeToString(h.Sum(nil))
	if len(hashStr) > 8 {
		return hashStr[:8]
	}
	return hashStr
}

func (s Session) TokenPrefix() string {
	return HashTokenPrefix(s.Token)
}

func (s Session) IsCurrentSession(currentToken string) bool {
	return s.Token != "" && s.Token == currentToken
}

func (s Session) LastConnected() string {
	if s.LastConnectedAt.IsZero() {
		return "Never"
	}
	return s.LastConnectedAt.Format("2006-01-02 15:04:05")
}

type Dashboard struct {
	DashboardID string
	UserID      string
	Slug        string
	Title       string
	AccessRules accessrules.AccessRules
}

func (d Dashboard) MaxDays() uint {
	if rule, ok := d.AccessRules["public"]; ok && rule.MaxDays > 0 {
		return rule.MaxDays
	}
	return 7
}

type PublicAgent a.PublicAgent

type Agent b.Agent

type DiskOdometer b.DiskOdometer

type Alert c.Alert

type AlertHistory c.AlertHistory

type AgentIdentity struct {
	UserID  string
	AgentID string
}

type PublicAgentIdentity struct {
	AgentID             string `json:"agent_id"`
	PublicAgentID       string `json:"public_agent_id"`
	PublicAgentNickname string `json:"public_agent_nickname"`
	SortKey             string `json:"sort_key"`
}

type AgentInfo struct {
	Nickname string
	IsOnline bool
	RamSize  uint64
	SwapSize uint64
	DiskSize uint64
}

type BeszelSSH struct {
	AgentID    string `json:"agent_id"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

type AgentManagement struct {
	AgentID         string `json:"agent_id"`
	AgentType       string `json:"agent_type"`
	Nickname        string `json:"nickname"`
	Token           string `json:"token"`
	BeszelPublicKey string `json:"beszel_public_key"`
}

// Per Ingestion disk info from Agent
type DiskDelta struct {
	Path       string
	TotalBytes uint64
	ReadBytes  uint64
	WriteBytes uint64
}
