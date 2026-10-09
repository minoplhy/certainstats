package store

import "context"

type IngestionRecord struct {
	ID, AgentID, UserID string
	Payload             []byte
	Complete            bool
}
type IngestionJournal interface {
	IngestionGet(context.Context, string) (*IngestionRecord, error)
	IngestionBegin(context.Context, IngestionRecord, uint64, uint64, []DiskDelta) error
	IngestionComplete(context.Context, string) error
	IngestionPending(context.Context) ([]IngestionRecord, error)
}
