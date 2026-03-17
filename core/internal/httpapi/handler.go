package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mdm/core/internal/infra"
	"mdm/core/internal/store"
)

const (
	headerRequestID = "X-Request-Id"
	headerUserID    = "X-User-Id"
	headerUserRole  = "X-User-Role"

	roleAdmin   = "mdm_admin"
	roleEditor  = "mdm_editor"
	roleViewer  = "mdm_viewer"
	apiBasePath = "/api/v1/"
)

type contextKey string

const (
	contextKeyRequestID contextKey = "request_id"
	contextKeyPrincipal contextKey = "principal"
)

type principal struct {
	UserExternalID string
	Roles          map[string]struct{}
	RawRoles       []string
}

type Handler struct {
	logger       *slog.Logger
	db           *sql.DB
	kafkaChecker *infra.KafkaChecker
	dictionaries *store.DictionaryRepository
	audit        *store.AuditRepository
}

func NewHandler(
	logger *slog.Logger,
	db *sql.DB,
	kafkaChecker *infra.KafkaChecker,
	dictionaries *store.DictionaryRepository,
	audit *store.AuditRepository,
) http.Handler {
	h := &Handler{
		logger:       logger,
		db:           db,
		kafkaChecker: kafkaChecker,
		dictionaries: dictionaries,
		audit:        audit,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.HandleFunc("GET /readyz", h.handleReady)

	mux.Handle("POST /api/v1/dictionaries", h.withRoles(http.HandlerFunc(h.handleCreateDictionary), roleEditor, roleAdmin))
	mux.Handle("GET /api/v1/dictionaries", h.withRoles(http.HandlerFunc(h.handleListDictionaries), roleViewer, roleEditor, roleAdmin))
	mux.Handle("GET /api/v1/dictionaries/{dictionary_id}", h.withRoles(http.HandlerFunc(h.handleGetDictionary), roleViewer, roleEditor, roleAdmin))
	mux.Handle("PATCH /api/v1/dictionaries/{dictionary_id}", h.withRoles(http.HandlerFunc(h.handleUpdateDictionary), roleEditor, roleAdmin))
	mux.Handle("DELETE /api/v1/dictionaries/{dictionary_id}", h.withRoles(http.HandlerFunc(h.handleDeleteDictionary), roleEditor, roleAdmin))

	return h.requestIDMiddleware(h.loggingMiddleware(h.authMiddleware(mux)))
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeData(w, r, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	postgresErr := h.db.PingContext(ctx)
	kafkaErr := h.kafkaChecker.Ping(ctx)

	status := http.StatusOK
	if postgresErr != nil || kafkaErr != nil {
		status = http.StatusServiceUnavailable
	}

	dependencies := map[string]string{
		"postgres": "ok",
		"kafka":    "ok",
	}
	if postgresErr != nil {
		dependencies["postgres"] = postgresErr.Error()
	}
	if kafkaErr != nil {
		dependencies["kafka"] = kafkaErr.Error()
	}

	writeData(w, r, status, map[string]any{
		"status":       strings.ToLower(http.StatusText(status)),
		"dependencies": dependencies,
	})
}

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

func (h *Handler) withRoles(next http.Handler, allowed ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "Missing authentication headers", nil)
			return
		}
		if !hasAnyRole(p, allowed...) {
			writeError(w, r, http.StatusForbidden, "forbidden", "Not enough permissions", map[string]any{
				"required_roles": allowed,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(headerRequestID)
		if strings.TrimSpace(requestID) == "" {
			requestID = newUUID()
		}
		w.Header().Set(headerRequestID, requestID)

		ctx := context.WithValue(r.Context(), contextKeyRequestID, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		h.logger.Info("request served",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(started),
			"request_id", requestIDFromContext(r.Context()),
		)
	})
}

func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, apiBasePath) {
			next.ServeHTTP(w, r)
			return
		}

		userID := strings.TrimSpace(r.Header.Get(headerUserID))
		roleHeader := strings.TrimSpace(r.Header.Get(headerUserRole))
		if userID == "" || roleHeader == "" {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "Missing X-User-Id or X-User-Role headers", nil)
			return
		}

		roles := splitRoles(roleHeader)
		if len(roles) == 0 {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "Header X-User-Role is empty", nil)
			return
		}

		roleSet := make(map[string]struct{}, len(roles))
		for _, role := range roles {
			roleSet[role] = struct{}{}
		}

		ctx := context.WithValue(r.Context(), contextKeyPrincipal, principal{
			UserExternalID: userID,
			Roles:          roleSet,
			RawRoles:       roles,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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

func writeData(w http.ResponseWriter, r *http.Request, status int, data any) {
	writeJSON(w, status, map[string]any{
		"request_id": requestIDFromContext(r.Context()),
		"data":       data,
	})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	writeJSON(w, status, map[string]any{
		"request_id": requestIDFromContext(r.Context()),
		"error": map[string]any{
			"code":    code,
			"message": message,
			"details": details,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("only one JSON object is allowed")
	}
	return nil
}

func parsePageParams(r *http.Request) (int, int, error) {
	const (
		defaultLimit = 50
		maxLimit     = 500
	)

	limit := defaultLimit
	offset := 0

	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxLimit {
			return 0, 0, fmt.Errorf("limit must be integer in range [1..%d]", maxLimit)
		}
		limit = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return 0, 0, errors.New("offset must be integer >= 0")
		}
		offset = parsed
	}

	return limit, offset, nil
}

func splitRoles(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		role := strings.TrimSpace(part)
		if role == "" {
			continue
		}
		result = append(result, role)
	}
	return result
}

func hasAnyRole(p principal, allowed ...string) bool {
	for _, role := range allowed {
		if _, ok := p.Roles[role]; ok {
			return true
		}
	}
	return false
}

func principalFromContext(ctx context.Context) (principal, bool) {
	value := ctx.Value(contextKeyPrincipal)
	if value == nil {
		return principal{}, false
	}
	p, ok := value.(principal)
	return p, ok
}

func requestIDFromContext(ctx context.Context) string {
	value := ctx.Value(contextKeyRequestID)
	if value == nil {
		return ""
	}
	requestID, _ := value.(string)
	return requestID
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	)
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, ch := range value {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if !isHex(ch) {
				return false
			}
		}
	}
	return true
}

func isHex(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}
