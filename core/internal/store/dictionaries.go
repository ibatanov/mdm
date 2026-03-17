package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Dictionary struct {
	ID            string  `json:"id"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Description   *string `json:"description,omitempty"`
	SchemaVersion int     `json:"schema_version"`
}

type DictionaryRepository struct {
	db *sql.DB
}

type CreateDictionaryInput struct {
	Code        string
	Name        string
	Description *string
}

type UpdateDictionaryInput struct {
	Name        *string
	Description *string
}

type ListResult struct {
	Items  []Dictionary `json:"items"`
	Total  int64        `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

func NewDictionaryRepository(db *sql.DB) *DictionaryRepository {
	return &DictionaryRepository{db: db}
}

func (r *DictionaryRepository) Create(ctx context.Context, input CreateDictionaryInput) (Dictionary, error) {
	const query = `
		INSERT INTO dictionaries (code, name, description)
		VALUES ($1, $2, $3)
		RETURNING id::text, code, name, description, schema_version
	`
	row := r.db.QueryRowContext(ctx, query, input.Code, input.Name, input.Description)
	result, err := scanDictionary(row)
	if err != nil {
		if isConflictError(err) {
			return Dictionary{}, ErrConflict
		}
		return Dictionary{}, fmt.Errorf("create dictionary: %w", err)
	}
	return result, nil
}

func (r *DictionaryRepository) List(ctx context.Context, limit, offset int) (ListResult, error) {
	const dataQuery = `
		SELECT
			id::text,
			code,
			name,
			description,
			schema_version
		FROM dictionaries
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := r.db.QueryContext(ctx, dataQuery, limit, offset)
	if err != nil {
		return ListResult{}, fmt.Errorf("list dictionaries query: %w", err)
	}
	defer rows.Close()

	items := make([]Dictionary, 0, limit)
	for rows.Next() {
		item, err := scanDictionary(rows)
		if err != nil {
			return ListResult{}, fmt.Errorf("scan dictionary: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("iterate dictionaries rows: %w", err)
	}

	var total int64
	const countQuery = `
		SELECT COUNT(*)
		FROM dictionaries
	`
	if err := r.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return ListResult{}, fmt.Errorf("count dictionaries: %w", err)
	}

	return ListResult{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (r *DictionaryRepository) GetByID(ctx context.Context, id string) (Dictionary, error) {
	const query = `
		SELECT
			id::text,
			code,
			name,
			description,
			schema_version
		FROM dictionaries
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	item, err := scanDictionary(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Dictionary{}, ErrNotFound
		}
		return Dictionary{}, fmt.Errorf("get dictionary: %w", err)
	}
	return item, nil
}

func (r *DictionaryRepository) UpdateByID(ctx context.Context, id string, input UpdateDictionaryInput) (Dictionary, error) {
	setParts := make([]string, 0, 4)
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

	setParts = append(setParts, "schema_version = schema_version + 1", "updated_at = now()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE dictionaries
		SET %s
		WHERE id = $%d
		RETURNING id::text, code, name, description, schema_version
	`, strings.Join(setParts, ", "), nextArg)

	row := r.db.QueryRowContext(ctx, query, args...)
	item, err := scanDictionary(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Dictionary{}, ErrNotFound
		}
		if isConflictError(err) {
			return Dictionary{}, ErrConflict
		}
		return Dictionary{}, fmt.Errorf("update dictionary: %w", err)
	}

	return item, nil
}

func (r *DictionaryRepository) DeleteByID(ctx context.Context, id string) error {
	const deleteQuery = `
		DELETE FROM dictionaries
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		if isConflictError(err) {
			return ErrConflict
		}
		return fmt.Errorf("delete dictionary: %w", err)
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

type scanner interface {
	Scan(dest ...any) error
}

func scanDictionary(s scanner) (Dictionary, error) {
	var item Dictionary
	var description sql.NullString
	if err := s.Scan(
		&item.ID,
		&item.Code,
		&item.Name,
		&description,
		&item.SchemaVersion,
	); err != nil {
		return Dictionary{}, err
	}
	if description.Valid {
		item.Description = &description.String
	}
	return item, nil
}

func isConflictError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" || pgErr.Code == "23503"
}
