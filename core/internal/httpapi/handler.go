package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"

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
	attributes   *store.AttributeRepository
	schemas      *store.DictionarySchemaRepository
	entries      *store.EntryRepository
	audit        *store.AuditRepository
}

func NewHandler(
	logger *slog.Logger,
	db *sql.DB,
	kafkaChecker *infra.KafkaChecker,
	dictionaries *store.DictionaryRepository,
	attributes *store.AttributeRepository,
	schemas *store.DictionarySchemaRepository,
	entries *store.EntryRepository,
	audit *store.AuditRepository,
) http.Handler {
	h := &Handler{
		logger:       logger,
		db:           db,
		kafkaChecker: kafkaChecker,
		dictionaries: dictionaries,
		attributes:   attributes,
		schemas:      schemas,
		entries:      entries,
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
	mux.Handle("POST /api/v1/attributes", h.withRoles(http.HandlerFunc(h.handleCreateAttribute), roleEditor, roleAdmin))
	mux.Handle("GET /api/v1/attributes", h.withRoles(http.HandlerFunc(h.handleListAttributes), roleViewer, roleEditor, roleAdmin))
	mux.Handle("GET /api/v1/attributes/{attribute_id}", h.withRoles(http.HandlerFunc(h.handleGetAttribute), roleViewer, roleEditor, roleAdmin))
	mux.Handle("PATCH /api/v1/attributes/{attribute_id}", h.withRoles(http.HandlerFunc(h.handleUpdateAttribute), roleEditor, roleAdmin))
	mux.Handle("DELETE /api/v1/attributes/{attribute_id}", h.withRoles(http.HandlerFunc(h.handleDeleteAttribute), roleEditor, roleAdmin))
	mux.Handle("GET /api/v1/dictionaries/{dictionary_id}/schema", h.withRoles(http.HandlerFunc(h.handleGetDictionarySchema), roleViewer, roleEditor, roleAdmin))
	mux.Handle("PUT /api/v1/dictionaries/{dictionary_id}/schema", h.withRoles(http.HandlerFunc(h.handlePutDictionarySchema), roleEditor, roleAdmin))
	mux.Handle("POST /api/v1/dictionaries/{dictionary_id}/entries", h.withRoles(http.HandlerFunc(h.handleCreateEntry), roleEditor, roleAdmin))
	mux.Handle("GET /api/v1/dictionaries/{dictionary_id}/entries", h.withRoles(http.HandlerFunc(h.handleListEntries), roleViewer, roleEditor, roleAdmin))
	mux.Handle("GET /api/v1/dictionaries/{dictionary_id}/entries/{entry_id}", h.withRoles(http.HandlerFunc(h.handleGetEntry), roleViewer, roleEditor, roleAdmin))
	mux.Handle("PATCH /api/v1/dictionaries/{dictionary_id}/entries/{entry_id}", h.withRoles(http.HandlerFunc(h.handleUpdateEntry), roleEditor, roleAdmin))
	mux.Handle("DELETE /api/v1/dictionaries/{dictionary_id}/entries/{entry_id}", h.withRoles(http.HandlerFunc(h.handleDeleteEntry), roleEditor, roleAdmin))
	mux.Handle("POST /api/v1/dictionaries/{dictionary_id}/entries/search", h.withRoles(http.HandlerFunc(h.handleSearchEntries), roleViewer, roleEditor, roleAdmin))

	return h.requestIDMiddleware(h.loggingMiddleware(h.authMiddleware(mux)))
}
