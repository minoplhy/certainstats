package base

type FindAgentByPublicID struct {
	Version     int64
	RulesJSON   string
	OwnerID     string
	RealAgentID string
}
