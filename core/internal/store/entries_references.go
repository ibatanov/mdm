package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const maxReferenceResolveDepth = 24

type dictionaryFieldMeta struct {
	DataType        string
	RefDictionaryID *string
	IsMultivalue    bool
}

type referenceResolver struct {
	repository          *EntryRepository
	fieldMetaCache      map[string]map[string]dictionaryFieldMeta
	entryCache          map[string]Entry
	resolvedDataByEntry map[string]map[string]any
}

type referenceSearchCandidate struct {
	EntryID string
	Tokens  []string
}

func newReferenceResolver(repository *EntryRepository) *referenceResolver {
	return &referenceResolver{
		repository:          repository,
		fieldMetaCache:      make(map[string]map[string]dictionaryFieldMeta),
		entryCache:          make(map[string]Entry),
		resolvedDataByEntry: make(map[string]map[string]any),
	}
}

func (r *EntryRepository) ResolveEntry(ctx context.Context, item Entry) (Entry, error) {
	resolver := newReferenceResolver(r)
	return resolver.resolveEntry(ctx, item)
}

func (r *EntryRepository) ResolveListEntriesResult(ctx context.Context, result ListEntriesResult) (ListEntriesResult, error) {
	if len(result.Items) == 0 {
		return result, nil
	}

	resolver := newReferenceResolver(r)
	resolvedItems := make([]Entry, 0, len(result.Items))
	for _, item := range result.Items {
		resolved, err := resolver.resolveEntry(ctx, item)
		if err != nil {
			return ListEntriesResult{}, err
		}
		resolvedItems = append(resolvedItems, resolved)
	}

	result.Items = resolvedItems
	return result, nil
}

func (resolver *referenceResolver) resolveEntry(ctx context.Context, item Entry) (Entry, error) {
	resolvedData, err := resolver.resolveData(ctx, item.DictionaryID, item.Data, 0, map[string]struct{}{})
	if err != nil {
		return Entry{}, err
	}

	item.Data = resolvedData
	return item, nil
}

func (resolver *referenceResolver) resolveData(
	ctx context.Context,
	dictionaryID string,
	data map[string]any,
	depth int,
	trail map[string]struct{},
) (map[string]any, error) {
	if data == nil {
		return map[string]any{}, nil
	}
	if depth > maxReferenceResolveDepth {
		return deepCloneMap(data), nil
	}

	fields, err := resolver.fieldMeta(ctx, dictionaryID)
	if err != nil {
		return nil, err
	}

	resolved := deepCloneMap(data)
	for field, rawValue := range data {
		meta, ok := fields[field]
		if !ok || meta.DataType != "reference" || meta.RefDictionaryID == nil {
			continue
		}

		value, err := resolver.resolveReferenceValue(ctx, *meta.RefDictionaryID, rawValue, depth+1, trail)
		if err != nil {
			return nil, err
		}
		resolved[field] = value
	}

	return resolved, nil
}

func (resolver *referenceResolver) resolveReferenceValue(
	ctx context.Context,
	referenceDictionaryID string,
	value any,
	depth int,
	trail map[string]struct{},
) (any, error) {
	switch typed := value.(type) {
	case string:
		return resolver.resolveReferenceByID(ctx, referenceDictionaryID, typed, depth, trail)
	case []any:
		items := make([]any, 0, len(typed))
		for _, raw := range typed {
			entryID, ok := raw.(string)
			if !ok {
				items = append(items, nil)
				continue
			}
			item, err := resolver.resolveReferenceByID(ctx, referenceDictionaryID, entryID, depth, trail)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, nil
	}
}

func (resolver *referenceResolver) resolveReferenceByID(
	ctx context.Context,
	referenceDictionaryID string,
	entryID string,
	depth int,
	trail map[string]struct{},
) (any, error) {
	if !isUUIDString(entryID) {
		return nil, nil
	}
	if depth > maxReferenceResolveDepth {
		return nil, nil
	}

	key := resolver.cacheKey(referenceDictionaryID, entryID)
	if cached, ok := resolver.resolvedDataByEntry[key]; ok {
		return deepCloneMap(cached), nil
	}
	if _, seen := trail[key]; seen {
		return nil, nil
	}

	entry, err := resolver.entry(ctx, referenceDictionaryID, entryID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	nextTrail := cloneStringSet(trail)
	nextTrail[key] = struct{}{}

	resolvedData, err := resolver.resolveData(ctx, referenceDictionaryID, entry.Data, depth+1, nextTrail)
	if err != nil {
		return nil, err
	}

	resolver.resolvedDataByEntry[key] = deepCloneMap(resolvedData)
	return deepCloneMap(resolvedData), nil
}

func (resolver *referenceResolver) fieldMeta(ctx context.Context, dictionaryID string) (map[string]dictionaryFieldMeta, error) {
	if cached, ok := resolver.fieldMetaCache[dictionaryID]; ok {
		return cached, nil
	}

	fields, err := resolver.repository.loadDictionaryFieldMeta(ctx, dictionaryID)
	if err != nil {
		return nil, err
	}
	resolver.fieldMetaCache[dictionaryID] = fields
	return fields, nil
}

func (resolver *referenceResolver) entry(ctx context.Context, dictionaryID, entryID string) (Entry, error) {
	key := resolver.cacheKey(dictionaryID, entryID)
	if cached, ok := resolver.entryCache[key]; ok {
		return cached, nil
	}

	item, err := resolver.repository.GetByID(ctx, dictionaryID, entryID)
	if err != nil {
		return Entry{}, err
	}

	resolver.entryCache[key] = item
	return item, nil
}

func (resolver *referenceResolver) cacheKey(dictionaryID, entryID string) string {
	return dictionaryID + ":" + entryID
}

func (r *EntryRepository) loadDictionaryFieldMeta(ctx context.Context, dictionaryID string) (map[string]dictionaryFieldMeta, error) {
	const query = `
		SELECT
			a.code,
			a.data_type,
			a.ref_dictionary_id::text,
			da.is_multivalue
		FROM dictionary_attributes da
		JOIN attributes a ON a.id = da.attribute_id
		WHERE da.dictionary_id = $1::uuid
	`

	rows, err := r.db.QueryContext(ctx, query, dictionaryID)
	if err != nil {
		return nil, fmt.Errorf("list dictionary field metadata: %w", err)
	}
	defer rows.Close()

	fields := make(map[string]dictionaryFieldMeta)
	for rows.Next() {
		var code string
		var dataType string
		var refDictionaryID sql.NullString
		var isMultivalue bool

		if err := rows.Scan(&code, &dataType, &refDictionaryID, &isMultivalue); err != nil {
			return nil, fmt.Errorf("scan dictionary field metadata: %w", err)
		}

		meta := dictionaryFieldMeta{
			DataType:     dataType,
			IsMultivalue: isMultivalue,
		}
		if refDictionaryID.Valid {
			value := refDictionaryID.String
			meta.RefDictionaryID = &value
		}
		fields[code] = meta
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dictionary field metadata rows: %w", err)
	}
	return fields, nil
}

func (r *EntryRepository) listAllByDictionaryID(ctx context.Context, dictionaryID string) ([]Entry, error) {
	const query = `
		SELECT
			id::text,
			dictionary_id::text,
			external_key,
			data,
			version
		FROM entries
		WHERE dictionary_id = $1::uuid
	`

	rows, err := r.db.QueryContext(ctx, query, dictionaryID)
	if err != nil {
		return nil, fmt.Errorf("list all entries by dictionary: %w", err)
	}
	defer rows.Close()

	items := make([]Entry, 0)
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan dictionary entries: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dictionary entries rows: %w", err)
	}
	return items, nil
}

func (r *EntryRepository) normalizeSearchFilters(
	ctx context.Context,
	dictionaryID string,
	filters []EntrySearchFilter,
) ([]EntrySearchFilter, bool, error) {
	if len(filters) == 0 {
		return nil, false, nil
	}

	resolver := newReferenceResolver(r)
	fields, err := resolver.fieldMeta(ctx, dictionaryID)
	if err != nil {
		return nil, false, fmt.Errorf("load dictionary schema for search: %w", err)
	}

	normalized := make([]EntrySearchFilter, 0, len(filters))
	candidatesCache := make(map[string][]referenceSearchCandidate)
	for _, filter := range filters {
		attribute := strings.TrimSpace(filter.Attribute)
		meta, ok := fields[attribute]
		if !ok || meta.DataType != "reference" || meta.RefDictionaryID == nil {
			normalized = append(normalized, filter)
			continue
		}

		transformed, drop, forceEmpty, err := r.normalizeReferenceFilter(
			ctx,
			resolver,
			attribute,
			*meta.RefDictionaryID,
			filter,
			candidatesCache,
		)
		if err != nil {
			return nil, false, err
		}
		if forceEmpty {
			return nil, true, nil
		}
		if drop {
			continue
		}

		normalized = append(normalized, transformed...)
	}

	return normalized, false, nil
}

func (r *EntryRepository) normalizeReferenceFilter(
	ctx context.Context,
	resolver *referenceResolver,
	attribute string,
	referenceDictionaryID string,
	filter EntrySearchFilter,
	candidatesCache map[string][]referenceSearchCandidate,
) ([]EntrySearchFilter, bool, bool, error) {
	op := strings.ToLower(strings.TrimSpace(filter.Op))

	switch op {
	case "eq":
		if filter.Value == nil {
			return nil, false, false, SearchValidationError{Message: "filter.value is required for eq"}
		}

		if value, ok := filter.Value.(string); ok && isUUIDString(strings.TrimSpace(value)) {
			return []EntrySearchFilter{{
				Attribute: attribute,
				Op:        "eq",
				Value:     strings.TrimSpace(value),
			}}, false, false, nil
		}

		token, ok := scalarSearchToken(filter.Value)
		if !ok {
			return nil, false, false, SearchValidationError{Message: "filter.value must be scalar for reference eq"}
		}

		ids, err := r.findReferenceIDsByPredicate(ctx, resolver, referenceDictionaryID, candidatesCache, func(candidate string) bool {
			return candidate == token
		})
		if err != nil {
			return nil, false, false, err
		}
		if len(ids) == 0 {
			return nil, false, true, nil
		}
		return []EntrySearchFilter{{
			Attribute: attribute,
			Op:        "in",
			Values:    stringSliceToAny(ids),
		}}, false, false, nil

	case "ne":
		if filter.Value == nil {
			return nil, false, false, SearchValidationError{Message: "filter.value is required for ne"}
		}

		if value, ok := filter.Value.(string); ok && isUUIDString(strings.TrimSpace(value)) {
			return []EntrySearchFilter{{
				Attribute: attribute,
				Op:        "ne",
				Value:     strings.TrimSpace(value),
			}}, false, false, nil
		}

		token, ok := scalarSearchToken(filter.Value)
		if !ok {
			return nil, false, false, SearchValidationError{Message: "filter.value must be scalar for reference ne"}
		}

		ids, err := r.findReferenceIDsByPredicate(ctx, resolver, referenceDictionaryID, candidatesCache, func(candidate string) bool {
			return candidate == token
		})
		if err != nil {
			return nil, false, false, err
		}
		if len(ids) == 0 {
			return nil, true, false, nil
		}

		transformed := make([]EntrySearchFilter, 0, len(ids))
		for _, id := range ids {
			transformed = append(transformed, EntrySearchFilter{
				Attribute: attribute,
				Op:        "ne",
				Value:     id,
			})
		}
		return transformed, false, false, nil

	case "in":
		if len(filter.Values) == 0 {
			return nil, false, false, SearchValidationError{Message: "filter.values is required for in"}
		}

		idsSet := make(map[string]struct{})
		for _, value := range filter.Values {
			if stringValue, ok := value.(string); ok && isUUIDString(strings.TrimSpace(stringValue)) {
				idsSet[strings.TrimSpace(stringValue)] = struct{}{}
				continue
			}

			token, ok := scalarSearchToken(value)
			if !ok {
				return nil, false, false, SearchValidationError{Message: "filter.values must contain scalar values for reference in"}
			}

			ids, err := r.findReferenceIDsByPredicate(ctx, resolver, referenceDictionaryID, candidatesCache, func(candidate string) bool {
				return candidate == token
			})
			if err != nil {
				return nil, false, false, err
			}
			for _, id := range ids {
				idsSet[id] = struct{}{}
			}
		}

		ids := mapKeys(idsSet)
		if len(ids) == 0 {
			return nil, false, true, nil
		}
		slices.Sort(ids)

		return []EntrySearchFilter{{
			Attribute: attribute,
			Op:        "in",
			Values:    stringSliceToAny(ids),
		}}, false, false, nil

	case "contains", "prefix":
		value, ok := filter.Value.(string)
		if !ok {
			return nil, false, false, SearchValidationError{Message: fmt.Sprintf("filter.value must be non-empty string for %s", op)}
		}
		needle := normalizeSearchToken(value)
		if needle == "" {
			return nil, false, false, SearchValidationError{Message: fmt.Sprintf("filter.value must be non-empty string for %s", op)}
		}

		ids, err := r.findReferenceIDsByPredicate(ctx, resolver, referenceDictionaryID, candidatesCache, func(candidate string) bool {
			if op == "contains" {
				return strings.Contains(candidate, needle)
			}
			return strings.HasPrefix(candidate, needle)
		})
		if err != nil {
			return nil, false, false, err
		}
		if len(ids) == 0 {
			return nil, false, true, nil
		}
		return []EntrySearchFilter{{
			Attribute: attribute,
			Op:        "in",
			Values:    stringSliceToAny(ids),
		}}, false, false, nil

	default:
		return nil, false, false, SearchValidationError{Message: "reference filters support only eq, ne, in, contains, prefix"}
	}
}

func (r *EntryRepository) findReferenceIDsByPredicate(
	ctx context.Context,
	resolver *referenceResolver,
	dictionaryID string,
	candidatesCache map[string][]referenceSearchCandidate,
	predicate func(candidate string) bool,
) ([]string, error) {
	candidates, err := r.referenceSearchCandidates(ctx, resolver, dictionaryID, candidatesCache)
	if err != nil {
		return nil, err
	}

	idsSet := make(map[string]struct{})
	for _, candidate := range candidates {
		for _, token := range candidate.Tokens {
			if predicate(token) {
				idsSet[candidate.EntryID] = struct{}{}
				break
			}
		}
	}

	ids := mapKeys(idsSet)
	slices.Sort(ids)
	return ids, nil
}

func (r *EntryRepository) referenceSearchCandidates(
	ctx context.Context,
	resolver *referenceResolver,
	dictionaryID string,
	candidatesCache map[string][]referenceSearchCandidate,
) ([]referenceSearchCandidate, error) {
	if cached, ok := candidatesCache[dictionaryID]; ok {
		return cached, nil
	}

	entries, err := r.listAllByDictionaryID(ctx, dictionaryID)
	if err != nil {
		return nil, err
	}

	result := make([]referenceSearchCandidate, 0, len(entries))
	for _, item := range entries {
		resolved, err := resolver.resolveEntry(ctx, item)
		if err != nil {
			return nil, err
		}

		tokens := flattenSearchTokens(resolved.Data)
		if item.ExternalKey != nil {
			token := normalizeSearchToken(*item.ExternalKey)
			if token != "" {
				tokens = append(tokens, token)
			}
		}
		tokens = uniqueStrings(tokens)

		result = append(result, referenceSearchCandidate{
			EntryID: item.ID,
			Tokens:  tokens,
		})
	}

	candidatesCache[dictionaryID] = result
	return result, nil
}

func flattenSearchTokens(value any) []string {
	result := make([]string, 0)
	collectSearchTokens(value, &result)
	return uniqueStrings(result)
}

func collectSearchTokens(value any, into *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for _, nested := range typed {
			collectSearchTokens(nested, into)
		}
	case []any:
		for _, nested := range typed {
			collectSearchTokens(nested, into)
		}
	case string:
		token := normalizeSearchToken(typed)
		if token != "" {
			*into = append(*into, token)
		}
	case bool:
		*into = append(*into, normalizeSearchToken(fmt.Sprint(typed)))
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		*into = append(*into, normalizeSearchToken(fmt.Sprint(typed)))
	}
}

func scalarSearchToken(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		token := normalizeSearchToken(typed)
		if token == "" {
			return "", false
		}
		return token, true
	case bool:
		return normalizeSearchToken(fmt.Sprint(typed)), true
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return normalizeSearchToken(fmt.Sprint(typed)), true
	default:
		return "", false
	}
}

func normalizeSearchToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func uniqueStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeSearchToken(value)
		if normalized == "" {
			continue
		}
		if _, exists := set[normalized]; exists {
			continue
		}
		set[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func stringSliceToAny(values []string) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return items
}

func mapKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}

func cloneStringSet(values map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(values))
	for key := range values {
		cloned[key] = struct{}{}
	}
	return cloned
}

func deepCloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}

	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = deepCloneAny(item)
	}
	return cloned
}

func deepCloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return deepCloneMap(typed)
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = deepCloneAny(item)
		}
		return items
	default:
		return typed
	}
}
