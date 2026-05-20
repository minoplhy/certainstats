package store

import (
	b "certainstats/internal/base/agent"
	c "certainstats/internal/base/alert"
	a "certainstats/internal/base/response"
	"certainstats/internal/dashboard/accessrules"
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

type Dashboard struct {
	DashboardID string
	UserID      string
	Slug        string
	Title       string
	AccessRules accessrules.AccessRules
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
