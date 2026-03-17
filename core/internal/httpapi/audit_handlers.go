package httpapi

import (
	"net/http"
	"strings"
	"time"

	"mdm/core/internal/store"
)

func (h *Handler) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePageParams(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	entityType := optionalTrimmedQuery(r, "entity_type")
	entityID := optionalTrimmedQuery(r, "entity_id")
	if entityID != nil && !isUUID(*entityID) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "entity_id must be UUID", nil)
		return
	}

	actorExternalID := optionalTrimmedQuery(r, "actor_external_id")

	occurredFrom, err := parseOptionalTimeQuery(r, "occurred_from")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	occurredTo, err := parseOptionalTimeQuery(r, "occurred_to")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if occurredFrom != nil && occurredTo != nil && occurredFrom.After(*occurredTo) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "occurred_from must be less than or equal to occurred_to", nil)
		return
	}

	result, err := h.audit.ListEvents(r.Context(), store.ListAuditEventsFilter{
		EntityType:      entityType,
		EntityID:        entityID,
		ActorExternalID: actorExternalID,
		OccurredFrom:    occurredFrom,
		OccurredTo:      occurredTo,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		h.logger.Error("list audit events failed", "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
		return
	}

	writeData(w, r, http.StatusOK, result)
}

func optionalTrimmedQuery(r *http.Request, key string) *string {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil
	}
	return &value
}

func parseOptionalTimeQuery(r *http.Request, key string) (*time.Time, error) {
	value := optionalTrimmedQuery(r, key)
	if value == nil {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
