package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type DictionarySchemaRepository struct {
	db *sql.DB
}

type DictionarySchemaAttribute struct {
	AttributeID  string `json:"attribute_id"`
	Required     bool   `json:"required"`
	DefaultValue any    `json:"default_value,omitempty"`
	Validators   any    `json:"validators,omitempty"`
	IsUnique     bool   `json:"is_unique"`
	IsMultivalue bool   `json:"is_multivalue"`
	Position     int    `json:"position"`
}

type ReplaceDictionarySchemaAttributeInput struct {
	AttributeID  string
	Required     bool
	DefaultValue any
	Validators   any
	IsUnique     bool
	IsMultivalue bool
	Position     int
}

func NewDictionarySchemaRepository(db *sql.DB) *DictionarySchemaRepository {
	return &DictionarySchemaRepository{db: db}
}

func (r *DictionarySchemaRepository) ListByDictionaryID(ctx context.Context, dictionaryID string) ([]DictionarySchemaAttribute, error) {
	if err := r.ensureDictionaryExists(ctx, dictionaryID); err != nil {
		return nil, err
	}

	const query = `
		SELECT
			attribute_id::text,
			required,
			default_value,
			validators,
			is_unique,
			is_multivalue,
			position
		FROM dictionary_attributes
		WHERE dictionary_id = $1
		ORDER BY position ASC, attribute_id ASC
	`
	rows, err := r.db.QueryContext(ctx, query, dictionaryID)
	if err != nil {
		return nil, fmt.Errorf("list dictionary schema: %w", err)
	}
	defer rows.Close()

	items := make([]DictionarySchemaAttribute, 0)
	for rows.Next() {
		item, err := scanDictionarySchemaAttribute(rows)
		if err != nil {
			return nil, fmt.Errorf("scan dictionary schema: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dictionary schema rows: %w", err)
	}

	return items, nil
}

func (r *DictionarySchemaRepository) FindMissingAttributeIDs(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	const query = `
		WITH requested AS (
			SELECT DISTINCT unnest($1::text[]) AS id
		),
		existing AS (
			SELECT id::text AS id
			FROM attributes
			WHERE id::text = ANY($1::text[])
		)
		SELECT requested.id
		FROM requested
		LEFT JOIN existing USING (id)
		WHERE existing.id IS NULL
		ORDER BY requested.id
	`
	rows, err := r.db.QueryContext(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("find missing attributes: %w", err)
	}
	defer rows.Close()

	missing := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan missing attribute id: %w", err)
		}
		missing = append(missing, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missing attribute ids: %w", err)
	}

	return missing, nil
}

func (r *DictionarySchemaRepository) ReplaceByDictionaryID(
	ctx context.Context,
	dictionaryID string,
	items []ReplaceDictionarySchemaAttributeInput,
) ([]DictionarySchemaAttribute, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if err := ensureDictionaryExistsTx(ctx, tx, dictionaryID); err != nil {
		return nil, err
	}

	const deleteQuery = `
		DELETE FROM dictionary_attributes
		WHERE dictionary_id = $1
	`
	if _, err := tx.ExecContext(ctx, deleteQuery, dictionaryID); err != nil {
		return nil, fmt.Errorf("delete dictionary schema: %w", err)
	}

	const insertQuery = `
		INSERT INTO dictionary_attributes (
			dictionary_id,
			attribute_id,
			required,
			default_value,
			validators,
			is_unique,
			is_multivalue,
			position
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4::jsonb,
			$5::jsonb,
			$6,
			$7,
			$8
		)
	`
	for _, item := range items {
		defaultValue, err := marshalJSON(item.DefaultValue)
		if err != nil {
			return nil, fmt.Errorf("marshal default_value for %s: %w", item.AttributeID, err)
		}
		validators, err := marshalJSON(item.Validators)
		if err != nil {
			return nil, fmt.Errorf("marshal validators for %s: %w", item.AttributeID, err)
		}

		if _, err := tx.ExecContext(
			ctx,
			insertQuery,
			dictionaryID,
			item.AttributeID,
			item.Required,
			defaultValue,
			validators,
			item.IsUnique,
			item.IsMultivalue,
			item.Position,
		); err != nil {
			if isConflictError(err) {
				return nil, ErrConflict
			}
			return nil, fmt.Errorf("insert dictionary schema: %w", err)
		}
	}

	const bumpVersionQuery = `
		UPDATE dictionaries
		SET
			schema_version = schema_version + 1,
			updated_at = now()
		WHERE id = $1
	`
	res, err := tx.ExecContext(ctx, bumpVersionQuery, dictionaryID)
	if err != nil {
		return nil, fmt.Errorf("bump dictionary schema version: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("schema version rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, ErrNotFound
	}

	itemsAfter, err := listDictionarySchemaTx(ctx, tx, dictionaryID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit schema update: %w", err)
	}
	tx = nil

	return itemsAfter, nil
}

func (r *DictionarySchemaRepository) ensureDictionaryExists(ctx context.Context, dictionaryID string) error {
	const query = `
		SELECT id
		FROM dictionaries
		WHERE id = $1
	`
	var id string
	if err := r.db.QueryRowContext(ctx, query, dictionaryID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("ensure dictionary exists: %w", err)
	}
	return nil
}

func ensureDictionaryExistsTx(ctx context.Context, tx *sql.Tx, dictionaryID string) error {
	const query = `
		SELECT id
		FROM dictionaries
		WHERE id = $1
	`
	var id string
	if err := tx.QueryRowContext(ctx, query, dictionaryID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("ensure dictionary exists in tx: %w", err)
	}
	return nil
}

func listDictionarySchemaTx(ctx context.Context, tx *sql.Tx, dictionaryID string) ([]DictionarySchemaAttribute, error) {
	const query = `
		SELECT
			attribute_id::text,
			required,
			default_value,
			validators,
			is_unique,
			is_multivalue,
			position
		FROM dictionary_attributes
		WHERE dictionary_id = $1
		ORDER BY position ASC, attribute_id ASC
	`
	rows, err := tx.QueryContext(ctx, query, dictionaryID)
	if err != nil {
		return nil, fmt.Errorf("list dictionary schema in tx: %w", err)
	}
	defer rows.Close()

	items := make([]DictionarySchemaAttribute, 0)
	for rows.Next() {
		item, err := scanDictionarySchemaAttribute(rows)
		if err != nil {
			return nil, fmt.Errorf("scan dictionary schema in tx: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dictionary schema rows in tx: %w", err)
	}
	return items, nil
}

func scanDictionarySchemaAttribute(s scanner) (DictionarySchemaAttribute, error) {
	var item DictionarySchemaAttribute
	var defaultValue []byte
	var validators []byte
	if err := s.Scan(
		&item.AttributeID,
		&item.Required,
		&defaultValue,
		&validators,
		&item.IsUnique,
		&item.IsMultivalue,
		&item.Position,
	); err != nil {
		return DictionarySchemaAttribute{}, err
	}

	value, err := unmarshalJSONValue(defaultValue)
	if err != nil {
		return DictionarySchemaAttribute{}, fmt.Errorf("unmarshal default_value: %w", err)
	}
	item.DefaultValue = value

	rules, err := unmarshalJSONValue(validators)
	if err != nil {
		return DictionarySchemaAttribute{}, fmt.Errorf("unmarshal validators: %w", err)
	}
	item.Validators = rules

	return item, nil
}

func unmarshalJSONValue(raw []byte) (any, error) {
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
