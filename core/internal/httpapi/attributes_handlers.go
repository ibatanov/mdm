package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"mdm/core/internal/store"
)

var allowedAttributeDataTypes = map[string]struct{}{
	"string":    {},
	"number":    {},
	"date":      {},
	"boolean":   {},
	"enum":      {},
	"reference": {},
}

func (h *Handler) handleCreateAttribute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code            string  `json:"code"`
		Name            string  `json:"name"`
		Description     *string `json:"description"`
		DataType        string  `json:"data_type"`
		RefDictionaryID *string `json:"ref_dictionary_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Malformed JSON body", map[string]any{"error": err.Error()})
		return
	}

	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.DataType = strings.TrimSpace(req.DataType)
	if req.Code == "" || req.Name == "" || req.DataType == "" {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Fields code, name and data_type are required", map[string]any{
			"code_required":      req.Code == "",
			"name_required":      req.Name == "",
			"data_type_required": req.DataType == "",
		})
		return
	}
	if _, ok := allowedAttributeDataTypes[req.DataType]; !ok {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Unsupported data_type", map[string]any{
			"data_type":     req.DataType,
			"allowed_types": dataTypesList(),
		})
		return
	}

	var refDictionaryID *string
	if req.RefDictionaryID != nil {
		trimmed := strings.TrimSpace(*req.RefDictionaryID)
		if trimmed != "" {
			if !isUUID(trimmed) {
				writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "ref_dictionary_id must be UUID", nil)
				return
			}
			refDictionaryID = &trimmed
		}
	}
	if req.DataType == "reference" && refDictionaryID == nil {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "ref_dictionary_id is required for reference data_type", nil)
		return
	}

	item, err := h.attributes.Create(r.Context(), store.CreateAttributeInput{
		Code:            req.Code,
		Name:            req.Name,
		Description:     req.Description,
		DataType:        req.DataType,
		RefDictionaryID: refDictionaryID,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Attribute conflicts with existing data", map[string]any{"code": req.Code})
		default:
			h.logger.Error("create attribute failed", "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditAttributeEvent(r, "attribute.created", item.ID, item.RefDictionaryID, nil, item)
	writeData(w, r, http.StatusCreated, item)
}

func (h *Handler) handleListAttributes(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePageParams(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	result, err := h.attributes.List(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("list attributes failed", "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	writeData(w, r, http.StatusOK, result)
}

func (h *Handler) handleGetAttribute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("attribute_id")
	if !isUUID(id) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "attribute_id must be UUID", nil)
		return
	}

	item, err := h.attributes.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Attribute not found", map[string]any{"attribute_id": id})
		default:
			h.logger.Error("get attribute failed", "attribute_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	writeData(w, r, http.StatusOK, item)
}

func (h *Handler) handleUpdateAttribute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("attribute_id")
	if !isUUID(id) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "attribute_id must be UUID", nil)
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

	before, err := h.attributes.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Attribute not found", map[string]any{"attribute_id": id})
		default:
			h.logger.Error("get attribute before update failed", "attribute_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	updated, err := h.attributes.UpdateByID(r.Context(), id, store.UpdateAttributeInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Attribute not found", map[string]any{"attribute_id": id})
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Attribute update conflicts with existing data", nil)
		default:
			h.logger.Error("update attribute failed", "attribute_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditAttributeEvent(r, "attribute.updated", id, updated.RefDictionaryID, before, updated)
	writeData(w, r, http.StatusOK, updated)
}

func (h *Handler) handleDeleteAttribute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("attribute_id")
	if !isUUID(id) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "attribute_id must be UUID", nil)
		return
	}

	before, err := h.attributes.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Attribute not found", map[string]any{"attribute_id": id})
		default:
			h.logger.Error("get attribute before delete failed", "attribute_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	if err := h.attributes.DeleteByID(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Attribute not found", map[string]any{"attribute_id": id})
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Attribute cannot be deleted due to related records", nil)
		default:
			h.logger.Error("delete attribute failed", "attribute_id", id, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditAttributeEvent(r, "attribute.deleted", id, before.RefDictionaryID, before, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) auditAttributeEvent(r *http.Request, action, attributeID string, dictionaryID *string, beforeState, afterState any) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		return
	}

	if err := h.audit.Write(r.Context(), store.AuditRecord{
		RequestID:       requestIDFromContext(r.Context()),
		ActorExternalID: p.UserExternalID,
		Action:          action,
		EntityType:      "attribute",
		EntityID:        attributeID,
		DictionaryID:    dictionaryID,
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
			"attribute_id", attributeID,
			"error", err,
		)
	}
}

func dataTypesList() []string {
	return []string{
		"string",
		"number",
		"date",
		"boolean",
		"enum",
		"reference",
	}
}
