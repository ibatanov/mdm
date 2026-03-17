package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"mdm/core/internal/store"
)

func (h *Handler) handleCreateDictionary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code        string  `json:"code"`
		Name        string  `json:"name"`
		Description *string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Malformed JSON body", map[string]any{"error": err.Error()})
		return
	}

	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	if req.Code == "" || req.Name == "" {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Fields code and name are required", map[string]any{
			"code_required": req.Code == "",
			"name_required": req.Name == "",
		})
		return
	}

	item, err := h.dictionaries.Create(r.Context(), store.CreateDictionaryInput{
		Code:        req.Code,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Dictionary code already exists", map[string]any{"code": req.Code})
		default:
			h.logger.Error("create dictionary failed", "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditDictionaryEvent(r, "dictionary.created", item.ID, nil, item)
	writeData(w, r, http.StatusCreated, item)
}

func (h *Handler) handleListDictionaries(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePageParams(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	result, err := h.dictionaries.List(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("list dictionaries failed", "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	writeData(w, r, http.StatusOK, result)
}

func (h *Handler) handleGetDictionary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("dictionary_id")
	if !isUUID(id) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	item, err := h.dictionaries.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Dictionary not found", map[string]any{"dictionary_id": id})
		default:
			h.logger.Error("get dictionary failed", "dictionary_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	writeData(w, r, http.StatusOK, item)
}

func (h *Handler) handleUpdateDictionary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("dictionary_id")
	if !isUUID(id) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Malformed JSON body", map[string]any{"error": err.Error()})
		return
	}

	if req.Name == nil && req.Description == nil {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "At least one field is required", nil)
		return
	}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Field name must be non-empty", nil)
			return
		}
		req.Name = &trimmed
	}

	before, err := h.dictionaries.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Dictionary not found", map[string]any{"dictionary_id": id})
		default:
			h.logger.Error("get dictionary before update failed", "dictionary_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	updated, err := h.dictionaries.UpdateByID(r.Context(), id, store.UpdateDictionaryInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Dictionary not found", map[string]any{"dictionary_id": id})
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Dictionary update conflicts with existing data", nil)
		default:
			h.logger.Error("update dictionary failed", "dictionary_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditDictionaryEvent(r, "dictionary.updated", id, before, updated)
	writeData(w, r, http.StatusOK, updated)
}

func (h *Handler) handleDeleteDictionary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("dictionary_id")
	if !isUUID(id) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	before, err := h.dictionaries.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Dictionary not found", map[string]any{"dictionary_id": id})
		default:
			h.logger.Error("get dictionary before delete failed", "dictionary_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	if err := h.dictionaries.DeleteByID(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Dictionary not found", map[string]any{"dictionary_id": id})
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Dictionary cannot be deleted due to related records", nil)
		default:
			h.logger.Error("delete dictionary failed", "dictionary_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditDictionaryEvent(r, "dictionary.deleted", id, before, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) auditDictionaryEvent(r *http.Request, action, dictionaryID string, beforeState, afterState any) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		return
	}

	if err := h.audit.Write(r.Context(), store.AuditRecord{
		RequestID:       requestIDFromContext(r.Context()),
		ActorExternalID: p.UserExternalID,
		Action:          action,
		EntityType:      "dictionary",
		EntityID:        dictionaryID,
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
			"error", err,
		)
	}
}
