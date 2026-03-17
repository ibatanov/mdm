package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"
)

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
		headerRequestIDValue := strings.TrimSpace(r.Header.Get(headerRequestID))
		responseRequestID := headerRequestIDValue
		if !isUUID(responseRequestID) {
			responseRequestID = newUUID()
		}
		w.Header().Set(headerRequestID, responseRequestID)

		ctx := context.WithValue(r.Context(), contextKeyRequestID, responseRequestID)
		requestWithContext := r.WithContext(ctx)

		if strings.HasPrefix(r.URL.Path, apiBasePath) {
			if headerRequestIDValue == "" {
				writeError(w, requestWithContext, http.StatusBadRequest, "invalid_request", "Missing X-Request-Id header", nil)
				return
			}
			if !isUUID(headerRequestIDValue) {
				writeError(w, requestWithContext, http.StatusBadRequest, "invalid_request", "X-Request-Id must be UUID", nil)
				return
			}
		}

		next.ServeHTTP(w, requestWithContext)
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

func principalFromContext(ctx context.Context) (principal, bool) {
	value := ctx.Value(contextKeyPrincipal)
	if value == nil {
		return principal{}, false
	}
	p, ok := value.(principal)
	return p, ok
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
