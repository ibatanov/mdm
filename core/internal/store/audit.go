package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type AuditRepository struct {
	db *sql.DB
}

type AuditRecord struct {
	RequestID       string
	ActorExternalID string
	Action          string
	EntityType      string
	EntityID        string
	DictionaryID    *string
	BeforeState     any
	AfterState      any
	Metadata        map[string]any
}

func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) Write(ctx context.Context, record AuditRecord) error {
	var requestID *string
	if strings.TrimSpace(record.RequestID) != "" {
		requestID = &record.RequestID
	}

	beforeState, err := marshalJSON(record.BeforeState)
	if err != nil {
		return fmt.Errorf("marshal before_state: %w", err)
	}
	afterState, err := marshalJSON(record.AfterState)
	if err != nil {
		return fmt.Errorf("marshal after_state: %w", err)
	}
	metadata, err := marshalJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	const query = `
		INSERT INTO audit_events (
			event_id,
			request_id,
			actor_external_id,
			action,
			entity_type,
			entity_id,
			dictionary_id,
			before_state,
			after_state,
			metadata
		)
		VALUES (
			uuid_generate_v4(),
			$1::uuid,
			$2,
			$3,
			$4,
			$5::uuid,
			$6::uuid,
			$7::jsonb,
			$8::jsonb,
			$9::jsonb
		)
	`
	_, err = r.db.ExecContext(
		ctx,
		query,
		requestID,
		record.ActorExternalID,
		record.Action,
		record.EntityType,
		record.EntityID,
		record.DictionaryID,
		beforeState,
		afterState,
		metadata,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func marshalJSON(value any) ([]byte, error) {
	if value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(value)
}
