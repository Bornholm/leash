package mcphttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestIPRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := newIPRateLimiter(rate.Every(time.Second), 5)
	for range 5 {
		if !rl.allow("1.2.3.4") {
			t.Fatal("expected allow under burst limit")
		}
	}
}

func TestIPRateLimiter_BlocksOverBurst(t *testing.T) {
	rl := newIPRateLimiter(rate.Every(time.Hour), 2)
	rl.allow("1.2.3.4")
	rl.allow("1.2.3.4")
	if rl.allow("1.2.3.4") {
		t.Fatal("expected block after burst exhausted")
	}
}

func TestIPRateLimiter_DistinctIPsAreIndependent(t *testing.T) {
	rl := newIPRateLimiter(rate.Every(time.Hour), 1)
	rl.allow("1.1.1.1")
	if !rl.allow("2.2.2.2") {
		t.Fatal("distinct IPs should have independent buckets")
	}
}

func TestIPRateLimiter_Cleanup(t *testing.T) {
	rl := newIPRateLimiter(rate.Every(time.Second), 10)
	rl.allow("1.2.3.4")

	rl.mu.Lock()
	rl.clients["1.2.3.4"].lastSeen = time.Now().Add(-20 * time.Minute)
	rl.mu.Unlock()

	rl.cleanup(10 * time.Minute)

	rl.mu.Lock()
	_, still := rl.clients["1.2.3.4"]
	rl.mu.Unlock()

	if still {
		t.Fatal("expected stale entry to be removed by cleanup")
	}
}

func TestHTTPRateLimitMiddleware_Returns429WhenExhausted(t *testing.T) {
	rl := newIPRateLimiter(rate.Every(time.Hour), 1)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := httpRateLimitMiddleware(rl, false, next)

	// Première requête : autorisée.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", rec1.Code)
	}

	// Deuxième requête : bucket épuisé → 429.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "10.0.0.1:5678"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status = %d, want 429", rec2.Code)
	}
}

func TestClientIP_NoProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:4321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	ip := clientIP(req, false)
	if ip != "192.168.1.1" {
		t.Fatalf("clientIP(trustProxy=false) = %q, want 192.168.1.1", ip)
	}
}

func TestClientIP_TrustProxy_XFF(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:4321"
	req.Header.Set("X-Forwarded-For", "5.6.7.8, 10.0.0.1")

	ip := clientIP(req, true)
	if ip != "5.6.7.8" {
		t.Fatalf("clientIP(trustProxy=true, XFF) = %q, want 5.6.7.8", ip)
	}
}

func TestClientIP_TrustProxy_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:4321"
	req.Header.Set("X-Real-IP", "9.10.11.12")

	ip := clientIP(req, true)
	if ip != "9.10.11.12" {
		t.Fatalf("clientIP(trustProxy=true, X-Real-IP) = %q, want 9.10.11.12", ip)
	}
}
