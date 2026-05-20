package agent

type ProvisionRequest struct {
	AgentType string `json:"agent_type"`
	Nickname  string `json:"nickname"`
}

type ProvisionMessage struct {
	Name        string `json:"name"`
	MessageType string `json:"message_type"` // "copy", "command", "note", "big_copy", "warning"
	Content     string `json:"content"`
	Description string `json:"description,omitempty"`
}



type ProvisionResponse struct {
	AgentID   string             `json:"agent_id"`
	Nickname  string             `json:"nickname"`
	AgentType string             `json:"agent_type"`
	Messages  []ProvisionMessage `json:"messages"`
	Message   string             `json:"message"`
}
