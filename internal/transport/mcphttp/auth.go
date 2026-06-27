package mcphttp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

type apiKeyContextKey struct{}

// extractAPIKey extrait la clé du header Authorization (schéma Bearer
// uniquement — C8). Le schéma est insensible à la casse (RFC 7235), le
// token est trimé.
func extractAPIKey(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	if len(h) < len(bearerPrefix) || !strings.EqualFold(h[:len(bearerPrefix)], bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(bearerPrefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// lookupAPIKey effectue une comparaison constant-time de rawKey contre
// chaque clé connue, en parcourant systématiquement toute la liste (pas de
// court-circuit) pour ne pas fuiter d'information temporelle sur la
// position de la clé qui matche.
func lookupAPIKey(rawKey string, keys []*APIKeyConfig) (*APIKeyConfig, bool) {
	candidate := sha256.Sum256([]byte(rawKey))
	var match *APIKeyConfig
	for _, k := range keys {
		if subtle.ConstantTimeCompare(candidate[:], k.keyHash[:]) == 1 {
			match = k
		}
	}
	return match, match != nil
}

// authMiddleware impose une authentification Authorization: Bearer
// exclusive (C8) : aucun fallback X-API-Key ni paramètre de query.
func authMiddleware(keys []*APIKeyConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawKey, ok := extractAPIKey(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="leash-mcp"`)
			http.Error(w, "missing or malformed Authorization header", http.StatusUnauthorized)
			return
		}

		cfg, found := lookupAPIKey(rawKey, keys)
		if !found {
			w.Header().Set("WWW-Authenticate", `Bearer realm="leash-mcp", error="invalid_token"`)
			http.Error(w, "invalid API key", http.StatusUnauthorized)
			return
		}

		ctx := contextWithAPIKey(r.Context(), cfg)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func contextWithAPIKey(ctx context.Context, cfg *APIKeyConfig) context.Context {
	return context.WithValue(ctx, apiKeyContextKey{}, cfg)
}

func apiKeyFromContext(ctx context.Context) (*APIKeyConfig, bool) {
	cfg, ok := ctx.Value(apiKeyContextKey{}).(*APIKeyConfig)
	return cfg, ok
}
