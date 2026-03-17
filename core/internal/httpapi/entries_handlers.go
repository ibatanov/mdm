package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"mdm/core/internal/store"
)

func (h *Handler) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	dictionaryID := r.PathValue("dictionary_id")
	if !isUUID(dictionaryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	if !h.ensureDictionaryExists(w, r, dictionaryID) {
		return
	}

	var req struct {
		ExternalKey *string        `json:"external_key"`
		Data        map[string]any `json:"data"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Malformed JSON body", map[string]any{"error": err.Error()})
		return
	}
	if req.Data == nil {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Field data is required", nil)
		return
	}

	externalKey := normalizeOptionalString(req.ExternalKey)
	if err := h.entries.ValidateData(r.Context(), dictionaryID, req.Data, nil); err != nil {
		if validationErr, ok := store.IsEntryValidationError(err); ok {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Entry data does not match dictionary schema", map[string]any{
				"issues": validationErr.Issues,
			})
			return
		}
		h.logger.Error("validate entry data failed", "dictionary_id", dictionaryID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	item, err := h.entries.Create(r.Context(), store.CreateEntryInput{
		DictionaryID: dictionaryID,
		ExternalKey:  externalKey,
		Data:         req.Data,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Entry conflicts with existing data", map[string]any{"external_key": externalKey})
		default:
			h.logger.Error("create entry failed", "dictionary_id", dictionaryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditEntryEvent(r, "entry.created", dictionaryID, item.ID, nil, item)

	resolvedItem, err := h.entries.ResolveEntry(r.Context(), item)
	if err != nil {
		h.logger.Error("resolve created entry references failed", "dictionary_id", dictionaryID, "entry_id", item.ID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	writeData(w, r, http.StatusCreated, resolvedItem)
}

func (h *Handler) handleListEntries(w http.ResponseWriter, r *http.Request) {
	dictionaryID := r.PathValue("dictionary_id")
	if !isUUID(dictionaryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	if !h.ensureDictionaryExists(w, r, dictionaryID) {
		return
	}

	limit, offset, err := parsePageParams(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	result, err := h.entries.ListByDictionaryID(r.Context(), dictionaryID, limit, offset)
	if err != nil {
		h.logger.Error("list entries failed", "dictionary_id", dictionaryID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	resolvedResult, err := h.entries.ResolveListEntriesResult(r.Context(), result)
	if err != nil {
		h.logger.Error("resolve listed entries references failed", "dictionary_id", dictionaryID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	writeData(w, r, http.StatusOK, resolvedResult)
}

func (h *Handler) handleGetEntry(w http.ResponseWriter, r *http.Request) {
	dictionaryID := r.PathValue("dictionary_id")
	if !isUUID(dictionaryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	entryID := r.PathValue("entry_id")
	if !isUUID(entryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "entry_id must be UUID", nil)
		return
	}

	if !h.ensureDictionaryExists(w, r, dictionaryID) {
		return
	}

	item, err := h.entries.GetByID(r.Context(), dictionaryID, entryID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Entry not found", map[string]any{
				"dictionary_id": dictionaryID,
				"entry_id":      entryID,
			})
		default:
			h.logger.Error("get entry failed", "dictionary_id", dictionaryID, "entry_id", entryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	resolvedItem, err := h.entries.ResolveEntry(r.Context(), item)
	if err != nil {
		h.logger.Error("resolve entry references failed", "dictionary_id", dictionaryID, "entry_id", entryID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	writeData(w, r, http.StatusOK, resolvedItem)
}

func (h *Handler) handleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	dictionaryID := r.PathValue("dictionary_id")
	if !isUUID(dictionaryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	entryID := r.PathValue("entry_id")
	if !isUUID(entryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "entry_id must be UUID", nil)
		return
	}

	if !h.ensureDictionaryExists(w, r, dictionaryID) {
		return
	}

	var req struct {
		Data map[string]any `json:"data"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Malformed JSON body", map[string]any{"error": err.Error()})
		return
	}
	if req.Data == nil {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Field data is required", nil)
		return
	}

	before, err := h.entries.GetByID(r.Context(), dictionaryID, entryID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Entry not found", map[string]any{
				"dictionary_id": dictionaryID,
				"entry_id":      entryID,
			})
		default:
			h.logger.Error("get entry before update failed", "dictionary_id", dictionaryID, "entry_id", entryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	mergedData := mergeEntryData(before.Data, req.Data)
	if err := h.entries.ValidateData(r.Context(), dictionaryID, mergedData, &entryID); err != nil {
		if validationErr, ok := store.IsEntryValidationError(err); ok {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Entry data does not match dictionary schema", map[string]any{
				"issues": validationErr.Issues,
			})
			return
		}
		h.logger.Error("validate entry data before update failed", "dictionary_id", dictionaryID, "entry_id", entryID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	updated, err := h.entries.UpdateByID(r.Context(), store.UpdateEntryInput{
		DictionaryID: dictionaryID,
		EntryID:      entryID,
		Data:         mergedData,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Entry not found", map[string]any{
				"dictionary_id": dictionaryID,
				"entry_id":      entryID,
			})
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Entry update conflicts with existing data", nil)
		default:
			h.logger.Error("update entry failed", "dictionary_id", dictionaryID, "entry_id", entryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditEntryEvent(r, "entry.updated", dictionaryID, entryID, before, updated)

	resolvedUpdated, err := h.entries.ResolveEntry(r.Context(), updated)
	if err != nil {
		h.logger.Error("resolve updated entry references failed", "dictionary_id", dictionaryID, "entry_id", entryID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	writeData(w, r, http.StatusOK, resolvedUpdated)
}

func (h *Handler) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	dictionaryID := r.PathValue("dictionary_id")
	if !isUUID(dictionaryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	entryID := r.PathValue("entry_id")
	if !isUUID(entryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "entry_id must be UUID", nil)
		return
	}

	if !h.ensureDictionaryExists(w, r, dictionaryID) {
		return
	}

	before, err := h.entries.GetByID(r.Context(), dictionaryID, entryID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Entry not found", map[string]any{
				"dictionary_id": dictionaryID,
				"entry_id":      entryID,
			})
		default:
			h.logger.Error("get entry before delete failed", "dictionary_id", dictionaryID, "entry_id", entryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	if err := h.entries.DeleteByID(r.Context(), dictionaryID, entryID); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Entry not found", map[string]any{
				"dictionary_id": dictionaryID,
				"entry_id":      entryID,
			})
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Entry cannot be deleted due to related records", nil)
		default:
			h.logger.Error("delete entry failed", "dictionary_id", dictionaryID, "entry_id", entryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditEntryEvent(r, "entry.deleted", dictionaryID, entryID, before, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSearchEntries(w http.ResponseWriter, r *http.Request) {
	dictionaryID := r.PathValue("dictionary_id")
	if !isUUID(dictionaryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	if !h.ensureDictionaryExists(w, r, dictionaryID) {
		return
	}

	var req struct {
		Filters []struct {
			Attribute string `json:"attribute"`
			Op        string `json:"op"`
			Value     any    `json:"value"`
			Values    []any  `json:"values"`
			From      any    `json:"from"`
			To        any    `json:"to"`
		} `json:"filters"`
		Sort []struct {
			Attribute string `json:"attribute"`
			Direction string `json:"direction"`
		} `json:"sort"`
		Page *struct {
			Limit  *int `json:"limit"`
			Offset *int `json:"offset"`
		} `json:"page"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Malformed JSON body", map[string]any{"error": err.Error()})
		return
	}

	limit, offset, err := parsePageFromSearchRequest(req.Page)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	filters := make([]store.EntrySearchFilter, 0, len(req.Filters))
	for _, filter := range req.Filters {
		filters = append(filters, store.EntrySearchFilter{
			Attribute: filter.Attribute,
			Op:        filter.Op,
			Value:     filter.Value,
			Values:    filter.Values,
			From:      filter.From,
			To:        filter.To,
		})
	}

	sort := make([]store.EntrySort, 0, len(req.Sort))
	for _, item := range req.Sort {
		sort = append(sort, store.EntrySort{
			Attribute: item.Attribute,
			Direction: item.Direction,
		})
	}

	result, err := h.entries.SearchByDictionaryID(r.Context(), store.SearchEntriesInput{
		DictionaryID: dictionaryID,
		Filters:      filters,
		Sort:         sort,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		switch {
		case store.IsSearchValidationError(err):
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", err.Error(), nil)
		default:
			h.logger.Error("search entries failed", "dictionary_id", dictionaryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	resolvedResult, err := h.entries.ResolveListEntriesResult(r.Context(), result)
	if err != nil {
		h.logger.Error("resolve searched entries references failed", "dictionary_id", dictionaryID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	writeData(w, r, http.StatusOK, resolvedResult)
}

func (h *Handler) ensureDictionaryExists(w http.ResponseWriter, r *http.Request, dictionaryID string) bool {
	_, err := h.dictionaries.GetByID(r.Context(), dictionaryID)
	if err == nil {
		return true
	}

	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Dictionary not found", map[string]any{"dictionary_id": dictionaryID})
	default:
		h.logger.Error("check dictionary exists failed", "dictionary_id", dictionaryID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
	}
	return false
}

func (h *Handler) auditEntryEvent(r *http.Request, action, dictionaryID, entryID string, beforeState, afterState any) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		return
	}

	if err := h.audit.Write(r.Context(), store.AuditRecord{
		RequestID:       requestIDFromContext(r.Context()),
		ActorExternalID: p.UserExternalID,
		Action:          action,
		EntityType:      "entry",
		EntityID:        entryID,
		DictionaryID:    &dictionaryID,
		BeforeState:     beforeState,
		AfterState:      afterState,
		Metadata: map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
			"roles":  p.RawRoles,
		},
	}); err != nil {
		h.logger.Error("failed to write audit event",
			"action", action,
			"dictionary_id", dictionaryID,
			"entry_id", entryID,
			"error", err,
		)
	}
}

func mergeEntryData(base, patch map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}

	result := make(map[string]any, len(base)+len(patch))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range patch {
		if value == nil {
			delete(result, key)
			continue
		}
		result[key] = value
	}
	return result
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func parsePageFromSearchRequest(page *struct {
	Limit  *int `json:"limit"`
	Offset *int `json:"offset"`
}) (int, int, error) {
	const (
		defaultLimit = 50
		maxLimit     = 500
	)

	limit := defaultLimit
	offset := 0
	if page == nil {
		return limit, offset, nil
	}

	if page.Limit != nil {
		if *page.Limit < 1 || *page.Limit > maxLimit {
			return 0, 0, errors.New("page.limit must be integer in range [1..500]")
		}
		limit = *page.Limit
	}
	if page.Offset != nil {
		if *page.Offset < 0 {
			return 0, 0, errors.New("page.offset must be integer >= 0")
		}
		offset = *page.Offset
	}
	return limit, offset, nil
}
