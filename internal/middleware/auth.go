package middleware

import (
	"net/http"
	"strings"

	"media-processor/pkg/response"
)

func APIKeyAuth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Si no hay API key configurada, permitir todo
			if apiKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Buscar en header Authorization: Bearer <key>
			auth := r.Header.Get("Authorization")
			if auth != "" {
				parts := strings.SplitN(auth, " ", 2)
				if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" && parts[1] == apiKey {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Buscar en header X-API-Key
			if r.Header.Get("X-API-Key") == apiKey {
				next.ServeHTTP(w, r)
				return
			}

			response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "API key invalida")
		})
	}
}
