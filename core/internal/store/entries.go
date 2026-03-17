package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Entry struct {
	ID           string         `json:"id"`
	DictionaryID string         `json:"dictionary_id"`
	ExternalKey  *string        `json:"external_key,omitempty"`
	Data         map[string]any `json:"data"`
	Version      int            `json:"version"`
}

type EntryRepository struct {
	db *sql.DB
}

type CreateEntryInput struct {
	DictionaryID string
	ExternalKey  *string
	Data         map[string]any
}

type UpdateEntryInput struct {
	DictionaryID string
	EntryID      string
	Data         map[string]any
}

type ListEntriesResult struct {
	Items  []Entry `json:"items"`
	Total  int64   `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

type SearchEntriesInput struct {
	DictionaryID string
	Filters      []EntrySearchFilter
	Sort         []EntrySort
	Limit        int
	Offset       int
}

type EntrySearchFilter struct {
	Attribute string
	Op        string
	Value     any
	Values    []any
	From      any
	To        any
}

type EntrySort struct {
	Attribute string
	Direction string
}

type SearchValidationError struct {
	Message string
}

func (e SearchValidationError) Error() string {
	return e.Message
}

func IsSearchValidationError(err error) bool {
	var validationErr SearchValidationError
	return errors.As(err, &validationErr)
}

func NewEntryRepository(db *sql.DB) *EntryRepository {
	return &EntryRepository{db: db}
}

func (r *EntryRepository) Create(ctx context.Context, input CreateEntryInput) (Entry, error) {
	data, err := marshalJSON(input.Data)
	if err != nil {
		return Entry{}, fmt.Errorf("marshal entry data: %w", err)
	}

	const query = `
		INSERT INTO entries (
			dictionary_id,
			external_key,
			data
		)
		VALUES ($1::uuid, $2, $3::jsonb)
		RETURNING
			id::text,
			dictionary_id::text,
			external_key,
			data,
			version
	`
	row := r.db.QueryRowContext(ctx, query, input.DictionaryID, input.ExternalKey, data)
	item, err := scanEntry(row)
	if err != nil {
		if isConflictError(err) {
			return Entry{}, ErrConflict
		}
		return Entry{}, fmt.Errorf("create entry: %w", err)
	}
	return item, nil
}

func (r *EntryRepository) ListByDictionaryID(ctx context.Context, dictionaryID string, limit, offset int) (ListEntriesResult, error) {
	const dataQuery = `
		SELECT
			id::text,
			dictionary_id::text,
			external_key,
			data,
			version
		FROM entries
		WHERE dictionary_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, dataQuery, dictionaryID, limit, offset)
	if err != nil {
		return ListEntriesResult{}, fmt.Errorf("list entries query: %w", err)
	}
	defer rows.Close()

	items := make([]Entry, 0, limit)
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return ListEntriesResult{}, fmt.Errorf("scan entry: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListEntriesResult{}, fmt.Errorf("iterate entries rows: %w", err)
	}

	var total int64
	const countQuery = `
		SELECT COUNT(*)
		FROM entries
		WHERE dictionary_id = $1::uuid
	`
	if err := r.db.QueryRowContext(ctx, countQuery, dictionaryID).Scan(&total); err != nil {
		return ListEntriesResult{}, fmt.Errorf("count entries: %w", err)
	}

	return ListEntriesResult{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (r *EntryRepository) GetByID(ctx context.Context, dictionaryID, entryID string) (Entry, error) {
	const query = `
		SELECT
			id::text,
			dictionary_id::text,
			external_key,
			data,
			version
		FROM entries
		WHERE dictionary_id = $1::uuid
		  AND id = $2::uuid
	`
	row := r.db.QueryRowContext(ctx, query, dictionaryID, entryID)
	item, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, fmt.Errorf("get entry: %w", err)
	}
	return item, nil
}

func (r *EntryRepository) UpdateByID(ctx context.Context, input UpdateEntryInput) (Entry, error) {
	data, err := marshalJSON(input.Data)
	if err != nil {
		return Entry{}, fmt.Errorf("marshal entry data: %w", err)
	}

	const query = `
		UPDATE entries
		SET
			data = $3::jsonb,
			version = version + 1,
			updated_at = now()
		WHERE dictionary_id = $1::uuid
		  AND id = $2::uuid
		RETURNING
			id::text,
			dictionary_id::text,
			external_key,
			data,
			version
	`
	row := r.db.QueryRowContext(ctx, query, input.DictionaryID, input.EntryID, data)
	item, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, ErrNotFound
		}
		if isConflictError(err) {
			return Entry{}, ErrConflict
		}
		return Entry{}, fmt.Errorf("update entry: %w", err)
	}
	return item, nil
}

func (r *EntryRepository) DeleteByID(ctx context.Context, dictionaryID, entryID string) error {
	const query = `
		DELETE FROM entries
		WHERE dictionary_id = $1::uuid
		  AND id = $2::uuid
	`
	result, err := r.db.ExecContext(ctx, query, dictionaryID, entryID)
	if err != nil {
		if isConflictError(err) {
			return ErrConflict
		}
		return fmt.Errorf("delete entry: %w", err)
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

func (r *EntryRepository) SearchByDictionaryID(ctx context.Context, input SearchEntriesInput) (ListEntriesResult, error) {
	whereClause, whereArgs, nextArg, err := buildEntriesSearchWhere(input.DictionaryID, input.Filters)
	if err != nil {
		return ListEntriesResult{}, err
	}

	orderClause, orderArgs, nextArg, err := buildEntriesSearchOrder(input.Sort, nextArg)
	if err != nil {
		return ListEntriesResult{}, err
	}

	dataArgs := make([]any, 0, len(whereArgs)+len(orderArgs)+2)
	dataArgs = append(dataArgs, whereArgs...)
	dataArgs = append(dataArgs, orderArgs...)
	dataArgs = append(dataArgs, input.Limit, input.Offset)

	dataQuery := fmt.Sprintf(`
		SELECT
			id::text,
			dictionary_id::text,
			external_key,
			data,
			version
		FROM entries
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, whereClause, orderClause, nextArg, nextArg+1)
	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return ListEntriesResult{}, fmt.Errorf("search entries query: %w", err)
	}
	defer rows.Close()

	items := make([]Entry, 0, input.Limit)
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return ListEntriesResult{}, fmt.Errorf("scan entry: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListEntriesResult{}, fmt.Errorf("iterate entries rows: %w", err)
	}

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM entries
		WHERE %s
	`, whereClause)
	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, whereArgs...).Scan(&total); err != nil {
		return ListEntriesResult{}, fmt.Errorf("count search entries: %w", err)
	}

	return ListEntriesResult{
		Items:  items,
		Total:  total,
		Limit:  input.Limit,
		Offset: input.Offset,
	}, nil
}

func buildEntriesSearchWhere(dictionaryID string, filters []EntrySearchFilter) (string, []any, int, error) {
	clauses := []string{
		"dictionary_id = $1::uuid",
	}
	args := []any{
		dictionaryID,
	}
	nextArg := 2

	for _, filter := range filters {
		clause, clauseArgs, newNextArg, err := buildEntriesFilterClause(filter, nextArg)
		if err != nil {
			return "", nil, 0, err
		}
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
		nextArg = newNextArg
	}

	return strings.Join(clauses, " AND "), args, nextArg, nil
}

func buildEntriesSearchOrder(sort []EntrySort, startArg int) (string, []any, int, error) {
	if len(sort) == 0 {
		return "created_at DESC", nil, startArg, nil
	}

	parts := make([]string, 0, len(sort)+1)
	args := make([]any, 0, len(sort))
	nextArg := startArg

	for _, item := range sort {
		attribute := strings.TrimSpace(item.Attribute)
		if attribute == "" {
			return "", nil, 0, SearchValidationError{Message: "sort.attribute must be non-empty"}
		}

		direction := strings.ToUpper(strings.TrimSpace(item.Direction))
		if direction == "" {
			direction = "ASC"
		}
		if direction != "ASC" && direction != "DESC" {
			return "", nil, 0, SearchValidationError{Message: "sort.direction must be asc or desc"}
		}

		parts = append(parts, fmt.Sprintf("(data ->> $%d) %s NULLS LAST", nextArg, direction))
		args = append(args, attribute)
		nextArg++
	}

	parts = append(parts, "id ASC")
	return strings.Join(parts, ", "), args, nextArg, nil
}

func buildEntriesFilterClause(filter EntrySearchFilter, startArg int) (string, []any, int, error) {
	attribute := strings.TrimSpace(filter.Attribute)
	if attribute == "" {
		return "", nil, 0, SearchValidationError{Message: "filter.attribute must be non-empty"}
	}

	op := strings.ToLower(strings.TrimSpace(filter.Op))
	if op == "" {
		return "", nil, 0, SearchValidationError{Message: "filter.op must be non-empty"}
	}

	switch op {
	case "eq":
		if filter.Value == nil {
			return "", nil, 0, SearchValidationError{Message: "filter.value is required for eq"}
		}
		value, err := marshalJSON(filter.Value)
		if err != nil {
			return "", nil, 0, fmt.Errorf("marshal eq value: %w", err)
		}
		clause := fmt.Sprintf("(data -> $%d) = $%d::jsonb", startArg, startArg+1)
		return clause, []any{attribute, value}, startArg + 2, nil

	case "ne":
		if filter.Value == nil {
			return "", nil, 0, SearchValidationError{Message: "filter.value is required for ne"}
		}
		value, err := marshalJSON(filter.Value)
		if err != nil {
			return "", nil, 0, fmt.Errorf("marshal ne value: %w", err)
		}
		clause := fmt.Sprintf("(data -> $%d) IS DISTINCT FROM $%d::jsonb", startArg, startArg+1)
		return clause, []any{attribute, value}, startArg + 2, nil

	case "contains":
		value, ok := filter.Value.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return "", nil, 0, SearchValidationError{Message: "filter.value must be non-empty string for contains"}
		}
		clause := fmt.Sprintf("(data ->> $%d) ILIKE ('%%' || $%d || '%%')", startArg, startArg+1)
		return clause, []any{attribute, value}, startArg + 2, nil

	case "prefix":
		value, ok := filter.Value.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return "", nil, 0, SearchValidationError{Message: "filter.value must be non-empty string for prefix"}
		}
		clause := fmt.Sprintf("(data ->> $%d) ILIKE ($%d || '%%')", startArg, startArg+1)
		return clause, []any{attribute, value}, startArg + 2, nil

	case "in":
		if len(filter.Values) == 0 {
			return "", nil, 0, SearchValidationError{Message: "filter.values is required for in"}
		}
		values, err := marshalJSON(filter.Values)
		if err != nil {
			return "", nil, 0, fmt.Errorf("marshal in values: %w", err)
		}
		clause := fmt.Sprintf(
			"EXISTS (SELECT 1 FROM jsonb_array_elements($%d::jsonb) AS candidate(value) WHERE (data -> $%d) = candidate.value)",
			startArg+1,
			startArg,
		)
		return clause, []any{attribute, values}, startArg + 2, nil

	case "lt", "lte", "gt", "gte":
		symbol := map[string]string{
			"lt":  "<",
			"lte": "<=",
			"gt":  ">",
			"gte": ">=",
		}[op]

		if number, ok := toFloat(filter.Value); ok {
			clause := fmt.Sprintf(
				"(CASE WHEN jsonb_typeof(data -> $%d) = 'number' THEN (data ->> $%d)::numeric %s $%d::numeric ELSE false END)",
				startArg,
				startArg,
				symbol,
				startArg+1,
			)
			return clause, []any{attribute, number}, startArg + 2, nil
		}

		value, ok := filter.Value.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return "", nil, 0, SearchValidationError{Message: "filter.value must be number or non-empty string for comparison ops"}
		}
		clause := fmt.Sprintf("(data ->> $%d) %s $%d", startArg, symbol, startArg+1)
		return clause, []any{attribute, value}, startArg + 2, nil

	case "range":
		if filter.From == nil || filter.To == nil {
			return "", nil, 0, SearchValidationError{Message: "filter.from and filter.to are required for range"}
		}

		fromNumber, fromNumberOK := toFloat(filter.From)
		toNumber, toNumberOK := toFloat(filter.To)
		if fromNumberOK && toNumberOK {
			clause := fmt.Sprintf(
				"(CASE WHEN jsonb_typeof(data -> $%d) = 'number' THEN (data ->> $%d)::numeric BETWEEN $%d::numeric AND $%d::numeric ELSE false END)",
				startArg,
				startArg,
				startArg+1,
				startArg+2,
			)
			return clause, []any{attribute, fromNumber, toNumber}, startArg + 3, nil
		}

		fromString, fromStringOK := filter.From.(string)
		toString, toStringOK := filter.To.(string)
		if fromStringOK && toStringOK && strings.TrimSpace(fromString) != "" && strings.TrimSpace(toString) != "" {
			clause := fmt.Sprintf("(data ->> $%d) BETWEEN $%d AND $%d", startArg, startArg+1, startArg+2)
			return clause, []any{attribute, fromString, toString}, startArg + 3, nil
		}

		return "", nil, 0, SearchValidationError{Message: "range supports only number-number or string-string bounds"}

	default:
		return "", nil, 0, SearchValidationError{Message: fmt.Sprintf("unsupported filter op: %s", op)}
	}
}

func scanEntry(s scanner) (Entry, error) {
	var item Entry
	var externalKey sql.NullString
	var dataRaw []byte
	if err := s.Scan(
		&item.ID,
		&item.DictionaryID,
		&externalKey,
		&dataRaw,
		&item.Version,
	); err != nil {
		return Entry{}, err
	}

	if externalKey.Valid {
		item.ExternalKey = &externalKey.String
	}

	if len(dataRaw) == 0 {
		item.Data = map[string]any{}
		return item, nil
	}

	var decoded map[string]any
	if err := json.Unmarshal(dataRaw, &decoded); err != nil {
		return Entry{}, fmt.Errorf("unmarshal entry data: %w", err)
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	item.Data = decoded
	return item, nil
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}
