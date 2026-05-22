package response

type PublicAgentReq struct {
	AgentID string `json:"agent_id"`
	Alias   string `json:"alias"` // Maps to agent_public_nickname
}

type CreateDashboardReq struct {
	Title         string                    `json:"title"`
	Slug          string                    `json:"slug"`
	AccessControl string                    `json:"access_control"`
	Agents        []CreateDashboardReqAgent `json:"agents"`
}

type CreateDashboardReqAgent struct {
	AgentID string `json:"agent_id"`
	Alias   string `json:"alias"`
	SortKey string `json:"sort_key"`
}
