package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type Attribute struct {
	ID              string  `json:"id"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	Description     *string `json:"description,omitempty"`
	DataType        string  `json:"data_type"`
	RefDictionaryID *string `json:"ref_dictionary_id,omitempty"`
}

type AttributeRepository struct {
	db *sql.DB
}

type CreateAttributeInput struct {
	Code            string
	Name            string
	Description     *string
	DataType        string
	RefDictionaryID *string
}

type UpdateAttributeInput struct {
	Name        *string
	Description *string
}

type AttributeListResult struct {
	Items  []Attribute `json:"items"`
	Total  int64       `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

func NewAttributeRepository(db *sql.DB) *AttributeRepository {
	return &AttributeRepository{db: db}
}

func (r *AttributeRepository) Create(ctx context.Context, input CreateAttributeInput) (Attribute, error) {
	const query = `
		INSERT INTO attributes (
			code,
			name,
			description,
			data_type,
			ref_dictionary_id
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING
			id::text,
			code,
			name,
			description,
			data_type,
			ref_dictionary_id::text
	`
	row := r.db.QueryRowContext(
		ctx,
		query,
		input.Code,
		input.Name,
		input.Description,
		input.DataType,
		input.RefDictionaryID,
	)
	result, err := scanAttribute(row)
	if err != nil {
		if isConflictError(err) {
			return Attribute{}, ErrConflict
		}
		return Attribute{}, fmt.Errorf("create attribute: %w", err)
	}
	return result, nil
}

func (r *AttributeRepository) List(ctx context.Context, limit, offset int) (AttributeListResult, error) {
	const dataQuery = `
		SELECT
			id::text,
			code,
			name,
			description,
			data_type,
			ref_dictionary_id::text
		FROM attributes
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := r.db.QueryContext(ctx, dataQuery, limit, offset)
	if err != nil {
		return AttributeListResult{}, fmt.Errorf("list attributes query: %w", err)
	}
	defer rows.Close()

	items := make([]Attribute, 0, limit)
	for rows.Next() {
		item, err := scanAttribute(rows)
		if err != nil {
			return AttributeListResult{}, fmt.Errorf("scan attribute: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AttributeListResult{}, fmt.Errorf("iterate attributes rows: %w", err)
	}

	var total int64
	const countQuery = `
		SELECT COUNT(*)
		FROM attributes
	`
	if err := r.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return AttributeListResult{}, fmt.Errorf("count attributes: %w", err)
	}

	return AttributeListResult{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (r *AttributeRepository) GetByID(ctx context.Context, id string) (Attribute, error) {
	const query = `
		SELECT
			id::text,
			code,
			name,
			description,
			data_type,
			ref_dictionary_id::text
		FROM attributes
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	item, err := scanAttribute(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Attribute{}, ErrNotFound
		}
		return Attribute{}, fmt.Errorf("get attribute: %w", err)
	}
	return item, nil
}

func (r *AttributeRepository) UpdateByID(ctx context.Context, id string, input UpdateAttributeInput) (Attribute, error) {
	setParts := make([]string, 0, 3)
	args := make([]any, 0, 3)
	nextArg := 1

	if input.Name != nil {
		setParts = append(setParts, fmt.Sprintf("name = $%d", nextArg))
		args = append(args, *input.Name)
		nextArg++
	}
	if input.Description != nil {
		setParts = append(setParts, fmt.Sprintf("description = $%d", nextArg))
		args = append(args, *input.Description)
		nextArg++
	}

	setParts = append(setParts, "updated_at = now()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE attributes
		SET %s
		WHERE id = $%d
		RETURNING
			id::text,
			code,
			name,
			description,
			data_type,
			ref_dictionary_id::text
	`, strings.Join(setParts, ", "), nextArg)

	row := r.db.QueryRowContext(ctx, query, args...)
	item, err := scanAttribute(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Attribute{}, ErrNotFound
		}
		if isConflictError(err) {
			return Attribute{}, ErrConflict
		}
		return Attribute{}, fmt.Errorf("update attribute: %w", err)
	}

	return item, nil
}

func (r *AttributeRepository) DeleteByID(ctx context.Context, id string) error {
	const deleteQuery = `
		DELETE FROM attributes
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		if isConflictError(err) {
			return ErrConflict
		}
		return fmt.Errorf("delete attribute: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func scanAttribute(s scanner) (Attribute, error) {
	var item Attribute
	var description sql.NullString
	var refDictionaryID sql.NullString
	if err := s.Scan(
		&item.ID,
		&item.Code,
		&item.Name,
		&description,
		&item.DataType,
		&refDictionaryID,
	); err != nil {
		return Attribute{}, err
	}
	if description.Valid {
		item.Description = &description.String
	}
	if refDictionaryID.Valid {
		item.RefDictionaryID = &refDictionaryID.String
	}
	return item, nil
}
