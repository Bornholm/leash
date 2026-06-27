package mcphttp

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAPIKeys() []*APIKeyConfig {
	return []*APIKeyConfig{
		{Name: "valid", keyHash: sha256.Sum256([]byte("valid-key"))},
	}
}

func TestAuthMiddleware_TableDriven(t *testing.T) {
	cases := []struct {
		name           string
		authHeader     string
		setNoHeader    bool
		wantStatus     int
		wantWWWAuth    bool
		wantWWWAuthErr string // sous-chaîne attendue dans WWW-Authenticate, si non vide
	}{
		{
			name:       "valid",
			authHeader: "Bearer valid-key",
			wantStatus: http.StatusOK,
		},
		{
			name:       "lowercase scheme",
			authHeader: "bearer valid-key",
			wantStatus: http.StatusOK,
		},
		{
			name:       "uppercase scheme",
			authHeader: "BEARER valid-key",
			wantStatus: http.StatusOK,
		},
		{
			name:       "mixed case scheme",
			authHeader: "BeArEr valid-key",
			wantStatus: http.StatusOK,
		},
		{
			name:       "extra spaces around token",
			authHeader: "Bearer   valid-key  ",
			wantStatus: http.StatusOK,
		},
		{
			name:           "absent header",
			setNoHeader:    true,
			wantStatus:     http.StatusUnauthorized,
			wantWWWAuth:    true,
			wantWWWAuthErr: "",
		},
		{
			name:           "wrong scheme",
			authHeader:     "Basic dXNlcjpwYXNz",
			wantStatus:     http.StatusUnauthorized,
			wantWWWAuth:    true,
			wantWWWAuthErr: "",
		},
		{
			name:           "bearer scheme without token",
			authHeader:     "Bearer ",
			wantStatus:     http.StatusUnauthorized,
			wantWWWAuth:    true,
			wantWWWAuthErr: "",
		},
		{
			name:           "bearer scheme with only spaces as token",
			authHeader:     "Bearer    ",
			wantStatus:     http.StatusUnauthorized,
			wantWWWAuth:    true,
			wantWWWAuthErr: "",
		},
		{
			name:           "unknown key",
			authHeader:     "Bearer not-the-right-key",
			wantStatus:     http.StatusUnauthorized,
			wantWWWAuth:    true,
			wantWWWAuthErr: "invalid_token",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calledWithCfg *APIKeyConfig
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calledWithCfg, _ = apiKeyFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			handler := authMiddleware(testAPIKeys(), next)

			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if !tc.setNoHeader {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}

			if tc.wantStatus == http.StatusOK {
				if calledWithCfg == nil || calledWithCfg.Name != "valid" {
					t.Fatalf("expected API key config in context, got %v", calledWithCfg)
				}
			}

			wwwAuth := rec.Header().Get("WWW-Authenticate")
			if tc.wantWWWAuth {
				if wwwAuth == "" {
					t.Fatal("expected WWW-Authenticate header on 401")
				}
				if tc.wantWWWAuthErr != "" && !strings.Contains(wwwAuth, tc.wantWWWAuthErr) {
					t.Fatalf("WWW-Authenticate = %q, want substring %q", wwwAuth, tc.wantWWWAuthErr)
				}
			}
		})
	}
}

func TestAuthMiddleware_NoXAPIKeyFallback(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(testAPIKeys(), next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("X-API-Key", "valid-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("X-API-Key must not be accepted as a fallback; status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddleware_NoQueryParamFallback(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(testAPIKeys(), next)

	req := httptest.NewRequest(http.MethodPost, "/mcp?api_key=valid-key", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("?api_key= must not be accepted as a fallback; status = %d, want 401", rec.Code)
	}
}
