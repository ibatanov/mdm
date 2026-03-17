package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
)

type EntryValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type EntryValidationError struct {
	Issues []EntryValidationIssue
}

func (e EntryValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "entry validation failed"
	}
	return e.Issues[0].Message
}

func IsEntryValidationError(err error) (EntryValidationError, bool) {
	var validationErr EntryValidationError
	ok := errors.As(err, &validationErr)
	return validationErr, ok
}

type schemaValidationAttribute struct {
	ID              string
	Code            string
	DataType        string
	RefDictionaryID *string
	Required        bool
	IsUnique        bool
	IsMultivalue    bool
	Validators      map[string]any
}

func (r *EntryRepository) ValidateData(ctx context.Context, dictionaryID string, data map[string]any, currentEntryID *string) error {
	attributes, err := r.loadValidationSchema(ctx, dictionaryID)
	if err != nil {
		return err
	}

	// Backward compatibility for dictionaries without configured schema.
	if len(attributes) == 0 {
		return nil
	}

	byCode := make(map[string]schemaValidationAttribute, len(attributes))
	for _, attribute := range attributes {
		byCode[attribute.Code] = attribute
	}

	issues := make([]EntryValidationIssue, 0)
	invalidAttributes := make(map[string]struct{})
	refCandidates := make(map[string]map[string]struct{})

	keys := mapsKeys(data)
	slices.Sort(keys)
	for _, key := range keys {
		value := data[key]

		attribute, ok := byCode[key]
		if !ok {
			issues = append(issues, EntryValidationIssue{
				Field:   key,
				Code:    "unknown_attribute",
				Message: fmt.Sprintf("Attribute %q is not configured in dictionary schema", key),
			})
			invalidAttributes[key] = struct{}{}
			continue
		}

		fieldIssues, refs := validateAttributeValue(attribute, key, value)
		if len(fieldIssues) > 0 {
			invalidAttributes[key] = struct{}{}
		}
		issues = append(issues, fieldIssues...)
		if len(fieldIssues) == 0 && len(refs) > 0 && attribute.RefDictionaryID != nil {
			dictionaryRefs := refCandidates[*attribute.RefDictionaryID]
			if dictionaryRefs == nil {
				dictionaryRefs = make(map[string]struct{}, len(refs))
				refCandidates[*attribute.RefDictionaryID] = dictionaryRefs
			}
			for _, ref := range refs {
				dictionaryRefs[ref] = struct{}{}
			}
		}
	}

	for _, attribute := range attributes {
		value, present := data[attribute.Code]
		if !attribute.Required {
			continue
		}
		if !present || value == nil {
			issues = append(issues, EntryValidationIssue{
				Field:   attribute.Code,
				Code:    "required",
				Message: fmt.Sprintf("Required attribute %q is missing", attribute.Code),
			})
			invalidAttributes[attribute.Code] = struct{}{}
		}
	}

	for _, attribute := range attributes {
		if !attribute.IsUnique {
			continue
		}
		if _, invalid := invalidAttributes[attribute.Code]; invalid {
			continue
		}

		value, present := data[attribute.Code]
		if !present || value == nil {
			continue
		}

		candidates, candidateIssues := uniqueCandidatesFromValue(attribute, attribute.Code, value)
		if len(candidateIssues) > 0 {
			issues = append(issues, candidateIssues...)
			invalidAttributes[attribute.Code] = struct{}{}
			continue
		}

		for _, candidate := range candidates {
			available, err := r.isAttributeValueUnique(ctx, dictionaryID, currentEntryID, attribute.Code, candidate)
			if err != nil {
				return fmt.Errorf("validate unique value for attribute %s: %w", attribute.Code, err)
			}
			if available {
				continue
			}
			issues = append(issues, EntryValidationIssue{
				Field:   attribute.Code,
				Code:    "not_unique",
				Message: fmt.Sprintf("Attribute %q value %s must be unique", attribute.Code, formatValidationValue(candidate)),
			})
		}
	}

	for refDictionaryID, refsSet := range refCandidates {
		refs := mapSetToSortedSlice(refsSet)
		missing, err := r.findMissingEntryIDs(ctx, refDictionaryID, refs)
		if err != nil {
			return fmt.Errorf("validate reference values: %w", err)
		}
		for _, missingID := range missing {
			issues = append(issues, EntryValidationIssue{
				Code:    "reference_not_found",
				Message: fmt.Sprintf("Referenced entry %q was not found in dictionary %q", missingID, refDictionaryID),
			})
		}
	}

	if len(issues) > 0 {
		return EntryValidationError{Issues: issues}
	}

	return nil
}

func (r *EntryRepository) loadValidationSchema(ctx context.Context, dictionaryID string) ([]schemaValidationAttribute, error) {
	const query = `
		SELECT
			a.id::text,
			a.code,
			a.data_type,
			a.ref_dictionary_id::text,
			da.required,
			da.is_unique,
			da.is_multivalue,
			da.validators
		FROM dictionary_attributes da
		INNER JOIN attributes a ON a.id = da.attribute_id
		WHERE da.dictionary_id = $1::uuid
		ORDER BY da.position ASC, a.code ASC
	`
	rows, err := r.db.QueryContext(ctx, query, dictionaryID)
	if err != nil {
		return nil, fmt.Errorf("load validation schema query: %w", err)
	}
	defer rows.Close()

	result := make([]schemaValidationAttribute, 0)
	for rows.Next() {
		var item schemaValidationAttribute
		var refDictionaryID sql.NullString
		var validatorsRaw []byte
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.DataType,
			&refDictionaryID,
			&item.Required,
			&item.IsUnique,
			&item.IsMultivalue,
			&validatorsRaw,
		); err != nil {
			return nil, fmt.Errorf("scan validation schema row: %w", err)
		}

		if refDictionaryID.Valid {
			item.RefDictionaryID = &refDictionaryID.String
		}

		if len(validatorsRaw) > 0 && !bytes.Equal(bytes.TrimSpace(validatorsRaw), []byte("null")) {
			var validators map[string]any
			if err := json.Unmarshal(validatorsRaw, &validators); err != nil {
				return nil, fmt.Errorf("unmarshal validators for attribute %s: %w", item.Code, err)
			}
			item.Validators = validators
		}

		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate validation schema rows: %w", err)
	}

	return result, nil
}

func (r *EntryRepository) findMissingEntryIDs(ctx context.Context, dictionaryID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	const query = `
		WITH requested AS (
			SELECT DISTINCT unnest($2::text[]) AS id
		),
		existing AS (
			SELECT id::text AS id
			FROM entries
			WHERE dictionary_id = $1::uuid
			  AND id::text = ANY($2::text[])
		)
		SELECT requested.id
		FROM requested
		LEFT JOIN existing USING (id)
		WHERE existing.id IS NULL
		ORDER BY requested.id
	`
	rows, err := r.db.QueryContext(ctx, query, dictionaryID, ids)
	if err != nil {
		return nil, fmt.Errorf("find missing entry ids query: %w", err)
	}
	defer rows.Close()

	missing := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan missing entry id: %w", err)
		}
		missing = append(missing, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missing entry ids rows: %w", err)
	}

	return missing, nil
}

func (r *EntryRepository) isAttributeValueUnique(
	ctx context.Context,
	dictionaryID string,
	currentEntryID *string,
	attributeCode string,
	value any,
) (bool, error) {
	jsonValue, err := marshalJSON(value)
	if err != nil {
		return false, fmt.Errorf("marshal unique candidate value: %w", err)
	}

	const query = `
		SELECT NOT EXISTS (
			SELECT 1
			FROM entries
			WHERE dictionary_id = $1::uuid
			  AND ($2::uuid IS NULL OR id <> $2::uuid)
			  AND (
				(data -> $3) = $4::jsonb
				OR (
					jsonb_typeof(data -> $3) = 'array'
					AND EXISTS (
						SELECT 1
						FROM jsonb_array_elements(data -> $3) AS item
						WHERE item = $4::jsonb
					)
				)
			  )
		)
	`

	var isUnique bool
	if err := r.db.QueryRowContext(ctx, query, dictionaryID, currentEntryID, attributeCode, jsonValue).Scan(&isUnique); err != nil {
		return false, err
	}
	return isUnique, nil
}

func validateAttributeValue(attribute schemaValidationAttribute, field string, value any) ([]EntryValidationIssue, []string) {
	if value == nil {
		return []EntryValidationIssue{{
			Field:   field,
			Code:    "null_not_allowed",
			Message: fmt.Sprintf("Attribute %q cannot be null", field),
		}}, nil
	}

	if attribute.IsMultivalue {
		rawItems, ok := value.([]any)
		if !ok {
			return []EntryValidationIssue{{
				Field:   field,
				Code:    "invalid_multivalue_type",
				Message: fmt.Sprintf("Attribute %q must be an array", field),
			}}, nil
		}

		issues := make([]EntryValidationIssue, 0)
		minItems, hasMinItems, err := validatorInt(attribute.Validators, "min_items")
		if err != nil {
			issues = append(issues, invalidValidatorIssue(field, "min_items", err))
		}
		maxItems, hasMaxItems, err := validatorInt(attribute.Validators, "max_items")
		if err != nil {
			issues = append(issues, invalidValidatorIssue(field, "max_items", err))
		}
		if hasMinItems && len(rawItems) < minItems {
			issues = append(issues, EntryValidationIssue{
				Field:   field,
				Code:    "min_items",
				Message: fmt.Sprintf("Attribute %q must contain at least %d items", field, minItems),
			})
		}
		if hasMaxItems && len(rawItems) > maxItems {
			issues = append(issues, EntryValidationIssue{
				Field:   field,
				Code:    "max_items",
				Message: fmt.Sprintf("Attribute %q must contain at most %d items", field, maxItems),
			})
		}
		if attribute.Required && len(rawItems) == 0 {
			issues = append(issues, EntryValidationIssue{
				Field:   field,
				Code:    "required",
				Message: fmt.Sprintf("Attribute %q must contain at least one value", field),
			})
		}

		references := make([]string, 0)
		for index, item := range rawItems {
			itemField := fmt.Sprintf("%s[%d]", field, index)
			itemIssues, ref := validateSingleValue(attribute, itemField, item)
			issues = append(issues, itemIssues...)
			if ref != nil {
				references = append(references, *ref)
			}
		}
		return issues, references
	}

	if _, isArray := value.([]any); isArray {
		return []EntryValidationIssue{{
			Field:   field,
			Code:    "invalid_single_value_type",
			Message: fmt.Sprintf("Attribute %q must be a single value", field),
		}}, nil
	}

	issues, reference := validateSingleValue(attribute, field, value)
	if reference == nil {
		return issues, nil
	}
	return issues, []string{*reference}
}

func uniqueCandidatesFromValue(attribute schemaValidationAttribute, field string, value any) ([]any, []EntryValidationIssue) {
	if !attribute.IsMultivalue {
		return []any{value}, nil
	}

	items, ok := value.([]any)
	if !ok {
		return nil, []EntryValidationIssue{{
			Field:   field,
			Code:    "invalid_multivalue_type",
			Message: fmt.Sprintf("Attribute %q must be an array", field),
		}}
	}

	issues := make([]EntryValidationIssue, 0)
	seen := make(map[string]struct{}, len(items))
	result := make([]any, 0, len(items))
	for index, item := range items {
		raw, err := marshalJSON(item)
		if err != nil {
			issues = append(issues, EntryValidationIssue{
				Field:   fmt.Sprintf("%s[%d]", field, index),
				Code:    "invalid_value",
				Message: fmt.Sprintf("Attribute %q item cannot be encoded", field),
			})
			continue
		}

		key := string(raw)
		if _, duplicate := seen[key]; duplicate {
			issues = append(issues, EntryValidationIssue{
				Field:   fmt.Sprintf("%s[%d]", field, index),
				Code:    "duplicate_value",
				Message: fmt.Sprintf("Attribute %q has duplicate value in array", field),
			})
			continue
		}

		seen[key] = struct{}{}
		result = append(result, item)
	}

	return result, issues
}

func validateSingleValue(attribute schemaValidationAttribute, field string, value any) ([]EntryValidationIssue, *string) {
	switch attribute.DataType {
	case "string":
		text, ok := value.(string)
		if !ok {
			return typeMismatchIssue(field, "string"), nil
		}
		issues := make([]EntryValidationIssue, 0)

		minLength, hasMinLength, err := validatorInt(attribute.Validators, "min_length")
		if err != nil {
			issues = append(issues, invalidValidatorIssue(field, "min_length", err))
		} else if hasMinLength && len([]rune(text)) < minLength {
			issues = append(issues, EntryValidationIssue{
				Field:   field,
				Code:    "min_length",
				Message: fmt.Sprintf("Attribute %q must be at least %d characters", field, minLength),
			})
		}

		maxLength, hasMaxLength, err := validatorInt(attribute.Validators, "max_length")
		if err != nil {
			issues = append(issues, invalidValidatorIssue(field, "max_length", err))
		} else if hasMaxLength && len([]rune(text)) > maxLength {
			issues = append(issues, EntryValidationIssue{
				Field:   field,
				Code:    "max_length",
				Message: fmt.Sprintf("Attribute %q must be at most %d characters", field, maxLength),
			})
		}

		pattern, hasPattern, err := validatorString(attribute.Validators, "pattern")
		if err != nil {
			issues = append(issues, invalidValidatorIssue(field, "pattern", err))
		} else if hasPattern {
			re, err := regexp.Compile(pattern)
			if err != nil {
				issues = append(issues, invalidValidatorIssue(field, "pattern", err))
			} else if !re.MatchString(text) {
				issues = append(issues, EntryValidationIssue{
					Field:   field,
					Code:    "pattern_mismatch",
					Message: fmt.Sprintf("Attribute %q does not match required pattern", field),
				})
			}
		}

		return issues, nil

	case "number":
		number, ok := toFloat(value)
		if !ok {
			return typeMismatchIssue(field, "number"), nil
		}

		issues := make([]EntryValidationIssue, 0)
		minValue, hasMinValue, err := validatorFloat(attribute.Validators, "min")
		if err != nil {
			issues = append(issues, invalidValidatorIssue(field, "min", err))
		} else if hasMinValue && number < minValue {
			issues = append(issues, EntryValidationIssue{
				Field:   field,
				Code:    "min",
				Message: fmt.Sprintf("Attribute %q must be >= %s", field, trimFloat(minValue)),
			})
		}

		maxValue, hasMaxValue, err := validatorFloat(attribute.Validators, "max")
		if err != nil {
			issues = append(issues, invalidValidatorIssue(field, "max", err))
		} else if hasMaxValue && number > maxValue {
			issues = append(issues, EntryValidationIssue{
				Field:   field,
				Code:    "max",
				Message: fmt.Sprintf("Attribute %q must be <= %s", field, trimFloat(maxValue)),
			})
		}

		return issues, nil

	case "boolean":
		if _, ok := value.(bool); !ok {
			return typeMismatchIssue(field, "boolean"), nil
		}
		return nil, nil

	case "date":
		text, ok := value.(string)
		if !ok {
			return typeMismatchIssue(field, "date (YYYY-MM-DD)"), nil
		}
		parsed, err := time.Parse("2006-01-02", text)
		if err != nil {
			return []EntryValidationIssue{{
				Field:   field,
				Code:    "invalid_date",
				Message: fmt.Sprintf("Attribute %q must be date in format YYYY-MM-DD", field),
			}}, nil
		}

		issues := make([]EntryValidationIssue, 0)
		minDate, hasMinDate, err := validatorDate(attribute.Validators, "min_date")
		if err != nil {
			issues = append(issues, invalidValidatorIssue(field, "min_date", err))
		} else if hasMinDate && parsed.Before(minDate) {
			issues = append(issues, EntryValidationIssue{
				Field:   field,
				Code:    "min_date",
				Message: fmt.Sprintf("Attribute %q must be on or after %s", field, minDate.Format("2006-01-02")),
			})
		}

		maxDate, hasMaxDate, err := validatorDate(attribute.Validators, "max_date")
		if err != nil {
			issues = append(issues, invalidValidatorIssue(field, "max_date", err))
		} else if hasMaxDate && parsed.After(maxDate) {
			issues = append(issues, EntryValidationIssue{
				Field:   field,
				Code:    "max_date",
				Message: fmt.Sprintf("Attribute %q must be on or before %s", field, maxDate.Format("2006-01-02")),
			})
		}

		return issues, nil

	case "enum":
		text, ok := value.(string)
		if !ok {
			return typeMismatchIssue(field, "enum string"), nil
		}

		allowedValues, configured, err := enumAllowedValues(attribute.Validators)
		if err != nil {
			return []EntryValidationIssue{invalidValidatorIssue(field, "allowed_values", err)}, nil
		}
		if configured {
			if _, ok := allowedValues[text]; !ok {
				values := mapSetToSortedSlice(allowedValues)
				return []EntryValidationIssue{{
					Field:   field,
					Code:    "enum_not_allowed",
					Message: fmt.Sprintf("Attribute %q value %q is not allowed (allowed: %s)", field, text, strings.Join(values, ", ")),
				}}, nil
			}
		}
		return nil, nil

	case "reference":
		text, ok := value.(string)
		if !ok {
			return typeMismatchIssue(field, "reference UUID string"), nil
		}
		if !isUUIDString(text) {
			return []EntryValidationIssue{{
				Field:   field,
				Code:    "invalid_reference_uuid",
				Message: fmt.Sprintf("Attribute %q must contain UUID reference", field),
			}}, nil
		}
		if attribute.RefDictionaryID == nil {
			return []EntryValidationIssue{{
				Field:   field,
				Code:    "invalid_schema",
				Message: fmt.Sprintf("Attribute %q has reference type but ref_dictionary_id is not configured", field),
			}}, nil
		}
		return nil, &text

	default:
		return []EntryValidationIssue{{
			Field:   field,
			Code:    "unsupported_data_type",
			Message: fmt.Sprintf("Attribute %q uses unsupported data_type %q", field, attribute.DataType),
		}}, nil
	}
}

func typeMismatchIssue(field, expected string) []EntryValidationIssue {
	return []EntryValidationIssue{{
		Field:   field,
		Code:    "type_mismatch",
		Message: fmt.Sprintf("Attribute %q must be %s", field, expected),
	}}
}

func invalidValidatorIssue(field, validator string, err error) EntryValidationIssue {
	return EntryValidationIssue{
		Field:   field,
		Code:    "invalid_validator",
		Message: fmt.Sprintf("Validator %q is invalid: %s", validator, err.Error()),
	}
}

func enumAllowedValues(validators map[string]any) (map[string]struct{}, bool, error) {
	if len(validators) == 0 {
		return nil, false, nil
	}

	candidates := []string{"allowed_values", "enum"}
	for _, key := range candidates {
		raw, exists := validators[key]
		if !exists {
			continue
		}

		items, ok := raw.([]any)
		if !ok {
			return nil, true, errors.New("must be array of strings")
		}

		result := make(map[string]struct{}, len(items))
		for _, item := range items {
			value, ok := item.(string)
			if !ok {
				return nil, true, errors.New("must contain only strings")
			}
			result[value] = struct{}{}
		}
		return result, true, nil
	}

	return nil, false, nil
}

func validatorInt(validators map[string]any, key string) (int, bool, error) {
	raw, exists := validators[key]
	if !exists {
		return 0, false, nil
	}

	number, ok := toFloat(raw)
	if !ok {
		return 0, true, errors.New("must be number")
	}
	if math.Trunc(number) != number {
		return 0, true, errors.New("must be integer")
	}
	return int(number), true, nil
}

func validatorFloat(validators map[string]any, key string) (float64, bool, error) {
	raw, exists := validators[key]
	if !exists {
		return 0, false, nil
	}

	number, ok := toFloat(raw)
	if !ok {
		return 0, true, errors.New("must be number")
	}
	return number, true, nil
}

func validatorString(validators map[string]any, key string) (string, bool, error) {
	raw, exists := validators[key]
	if !exists {
		return "", false, nil
	}

	value, ok := raw.(string)
	if !ok {
		return "", true, errors.New("must be string")
	}
	return value, true, nil
}

func validatorDate(validators map[string]any, key string) (time.Time, bool, error) {
	value, exists, err := validatorString(validators, key)
	if err != nil || !exists {
		return time.Time{}, exists, err
	}

	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, true, errors.New("must be date in format YYYY-MM-DD")
	}
	return parsed, true, nil
}

func formatValidationValue(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "<unserializable>"
	}
	return string(raw)
}

func trimFloat(value float64) string {
	if math.Trunc(value) == value {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%g", value)
}

func mapSetToSortedSlice(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func mapsKeys(values map[string]any) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}

func isUUIDString(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, ch := range value {
		switch index {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
				return false
			}
		}
	}
	return true
}
