package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"
)

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
