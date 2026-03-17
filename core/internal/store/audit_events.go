package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AuditEvent struct {
	EventID         string    `json:"event_id"`
	RequestID       *string   `json:"request_id,omitempty"`
	ActorExternalID *string   `json:"actor_external_id,omitempty"`
	ActorType       string    `json:"actor_type"`
	Action          string    `json:"action"`
	EntityType      string    `json:"entity_type"`
	EntityID        *string   `json:"entity_id,omitempty"`
	DictionaryID    *string   `json:"dictionary_id,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
	BeforeState     any       `json:"before_state,omitempty"`
	AfterState      any       `json:"after_state,omitempty"`
	Metadata        any       `json:"metadata,omitempty"`
}

type ListAuditEventsFilter struct {
	EntityType      *string
	EntityID        *string
	ActorExternalID *string
	OccurredFrom    *time.Time
	OccurredTo      *time.Time
	Limit           int
	Offset          int
}

type ListAuditEventsResult struct {
	Items  []AuditEvent `json:"items"`
	Total  int64        `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

func (r *AuditRepository) ListEvents(ctx context.Context, filter ListAuditEventsFilter) (ListAuditEventsResult, error) {
	whereParts := []string{
		"1 = 1",
	}
	args := make([]any, 0, 8)
	nextArg := 1

	if filter.EntityType != nil {
		whereParts = append(whereParts, fmt.Sprintf("entity_type = $%d", nextArg))
		args = append(args, *filter.EntityType)
		nextArg++
	}
	if filter.EntityID != nil {
		whereParts = append(whereParts, fmt.Sprintf("entity_id = $%d::uuid", nextArg))
		args = append(args, *filter.EntityID)
		nextArg++
	}
	if filter.ActorExternalID != nil {
		whereParts = append(whereParts, fmt.Sprintf("actor_external_id = $%d", nextArg))
		args = append(args, *filter.ActorExternalID)
		nextArg++
	}
	if filter.OccurredFrom != nil {
		whereParts = append(whereParts, fmt.Sprintf("occurred_at >= $%d", nextArg))
		args = append(args, *filter.OccurredFrom)
		nextArg++
	}
	if filter.OccurredTo != nil {
		whereParts = append(whereParts, fmt.Sprintf("occurred_at <= $%d", nextArg))
		args = append(args, *filter.OccurredTo)
		nextArg++
	}

	whereClause := strings.Join(whereParts, " AND ")

	dataQuery := fmt.Sprintf(`
		SELECT
			event_id::text,
			request_id::text,
			actor_external_id,
			actor_type,
			action,
			entity_type,
			entity_id::text,
			dictionary_id::text,
			occurred_at,
			before_state,
			after_state,
			metadata
		FROM audit_events
		WHERE %s
		ORDER BY occurred_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, nextArg, nextArg+1)

	dataArgs := make([]any, 0, len(args)+2)
	dataArgs = append(dataArgs, args...)
	dataArgs = append(dataArgs, filter.Limit, filter.Offset)

	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return ListAuditEventsResult{}, fmt.Errorf("list audit events query: %w", err)
	}
	defer rows.Close()

	items := make([]AuditEvent, 0, filter.Limit)
	for rows.Next() {
		item, err := scanAuditEvent(rows)
		if err != nil {
			return ListAuditEventsResult{}, fmt.Errorf("scan audit event: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListAuditEventsResult{}, fmt.Errorf("iterate audit event rows: %w", err)
	}

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM audit_events
		WHERE %s
	`, whereClause)
	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return ListAuditEventsResult{}, fmt.Errorf("count audit events: %w", err)
	}

	return ListAuditEventsResult{
		Items:  items,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}, nil
}

func scanAuditEvent(s scanner) (AuditEvent, error) {
	var item AuditEvent
	var requestID sql.NullString
	var actorExternalID sql.NullString
	var entityID sql.NullString
	var dictionaryID sql.NullString
	var beforeState []byte
	var afterState []byte
	var metadata []byte

	if err := s.Scan(
		&item.EventID,
		&requestID,
		&actorExternalID,
		&item.ActorType,
		&item.Action,
		&item.EntityType,
		&entityID,
		&dictionaryID,
		&item.OccurredAt,
		&beforeState,
		&afterState,
		&metadata,
	); err != nil {
		return AuditEvent{}, err
	}

	if requestID.Valid {
		item.RequestID = &requestID.String
	}
	if actorExternalID.Valid {
		item.ActorExternalID = &actorExternalID.String
	}
	if entityID.Valid {
		item.EntityID = &entityID.String
	}
	if dictionaryID.Valid {
		item.DictionaryID = &dictionaryID.String
	}

	before, err := unmarshalNullableJSON(beforeState)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("unmarshal before_state: %w", err)
	}
	item.BeforeState = before

	after, err := unmarshalNullableJSON(afterState)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("unmarshal after_state: %w", err)
	}
	item.AfterState = after

	meta, err := unmarshalNullableJSON(metadata)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("unmarshal metadata: %w", err)
	}
	item.Metadata = meta

	return item, nil
}

func unmarshalNullableJSON(raw []byte) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}
