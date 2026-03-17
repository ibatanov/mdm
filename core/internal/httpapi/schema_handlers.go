package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"mdm/core/internal/store"
)

func (h *Handler) handleGetDictionarySchema(w http.ResponseWriter, r *http.Request) {
	dictionaryID := r.PathValue("dictionary_id")
	if !isUUID(dictionaryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	attributes, err := h.schemas.ListByDictionaryID(r.Context(), dictionaryID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Dictionary not found", map[string]any{"dictionary_id": dictionaryID})
		default:
			h.logger.Error("get dictionary schema failed", "dictionary_id", dictionaryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	writeData(w, r, http.StatusOK, map[string]any{
		"attributes": attributes,
	})
}

func (h *Handler) handlePutDictionarySchema(w http.ResponseWriter, r *http.Request) {
	dictionaryID := r.PathValue("dictionary_id")
	if !isUUID(dictionaryID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "dictionary_id must be UUID", nil)
		return
	}

	var req struct {
		Attributes *[]struct {
			AttributeID  string `json:"attribute_id"`
			Required     bool   `json:"required"`
			DefaultValue any    `json:"default_value"`
			Validators   any    `json:"validators"`
			IsUnique     bool   `json:"is_unique"`
			IsMultivalue bool   `json:"is_multivalue"`
			Position     int    `json:"position"`
		} `json:"attributes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Malformed JSON body", map[string]any{"error": err.Error()})
		return
	}
	if req.Attributes == nil {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Field attributes is required", nil)
		return
	}

	attributesIn := *req.Attributes
	input := make([]store.ReplaceDictionarySchemaAttributeInput, 0, len(attributesIn))
	attributeIDs := make([]string, 0, len(attributesIn))
	seenIDs := make(map[string]struct{}, len(attributesIn))

	for idx, attribute := range attributesIn {
		attributeID := strings.TrimSpace(attribute.AttributeID)
		if !isUUID(attributeID) {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "attribute_id must be UUID", map[string]any{
				"index": idx,
			})
			return
		}
		if attribute.Position < 0 {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "position must be >= 0", map[string]any{
				"index":    idx,
				"position": attribute.Position,
			})
			return
		}
		if _, ok := seenIDs[attributeID]; ok {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "attribute_id must be unique in schema", map[string]any{
				"attribute_id": attributeID,
			})
			return
		}

		seenIDs[attributeID] = struct{}{}
		attributeIDs = append(attributeIDs, attributeID)
		input = append(input, store.ReplaceDictionarySchemaAttributeInput{
			AttributeID:  attributeID,
			Required:     attribute.Required,
			DefaultValue: attribute.DefaultValue,
			Validators:   attribute.Validators,
			IsUnique:     attribute.IsUnique,
			IsMultivalue: attribute.IsMultivalue,
			Position:     attribute.Position,
		})
	}

	before, err := h.schemas.ListByDictionaryID(r.Context(), dictionaryID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Dictionary not found", map[string]any{"dictionary_id": dictionaryID})
		default:
			h.logger.Error("get dictionary schema before update failed", "dictionary_id", dictionaryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	missing, err := h.schemas.FindMissingAttributeIDs(r.Context(), attributeIDs)
	if err != nil {
		h.logger.Error("validate dictionary schema attributes failed", "dictionary_id", dictionaryID, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}
	if len(missing) > 0 {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "Some attributes do not exist", map[string]any{
			"missing_attribute_ids": missing,
		})
		return
	}

	updated, err := h.schemas.ReplaceByDictionaryID(r.Context(), dictionaryID, input)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "Dictionary not found", map[string]any{"dictionary_id": dictionaryID})
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, "conflict", "Schema update conflicts with existing data", nil)
		default:
			h.logger.Error("update dictionary schema failed", "dictionary_id", dictionaryID, "error", err)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		}
		return
	}

	h.auditDictionarySchemaEvent(r, dictionaryID, before, updated)
	writeData(w, r, http.StatusOK, map[string]any{
		"attributes": updated,
	})
}

func (h *Handler) auditDictionarySchemaEvent(r *http.Request, dictionaryID string, beforeState, afterState any) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		return
	}

	if err := h.audit.Write(r.Context(), store.AuditRecord{
		RequestID:       requestIDFromContext(r.Context()),
		ActorExternalID: p.UserExternalID,
		Action:          "dictionary.schema.updated",
		EntityType:      "dictionary_schema",
		EntityID:        dictionaryID,
		DictionaryID:    &dictionaryID,
		BeforeState: map[string]any{
			"attributes": beforeState,
		},
		AfterState: map[string]any{
			"attributes": afterState,
		},
		Metadata: map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
			"roles":  p.RawRoles,
		},
	}); err != nil {
		h.logger.Error("failed to write audit event",
			"action", "dictionary.schema.updated",
			"dictionary_id", dictionaryID,
			"error", err,
		)
	}
}
