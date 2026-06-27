package mcphttp

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// ipRateLimiter gère un token-bucket rate.Limiter par adresse IP cliente.
// Thread-safe.
type ipRateLimiter struct {
	mu      sync.Mutex
	clients map[string]*ipEntry
	r       rate.Limit
	burst   int
}

func newIPRateLimiter(r rate.Limit, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		clients: make(map[string]*ipEntry),
		r:       r,
		burst:   burst,
	}
}

func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	e, ok := rl.clients[ip]
	if !ok {
		e = &ipEntry{lim: rate.NewLimiter(rl.r, rl.burst)}
		rl.clients[ip] = e
	}
	e.lastSeen = time.Now()
	allowed := e.lim.Allow()
	rl.mu.Unlock()
	return allowed
}

// cleanup supprime les entrées inactives depuis plus de maxAge.
func (rl *ipRateLimiter) cleanup(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for ip, e := range rl.clients {
		if e.lastSeen.Before(cutoff) {
			delete(rl.clients, ip)
		}
	}
}

// clientIP extrait l'adresse IP réelle du client. Si trustProxy est vrai,
// X-Forwarded-For (première entrée) puis X-Real-IP sont consultés avant
// RemoteAddr. Ne jamais activer trustProxy sans un reverse proxy qui contrôle
// ces headers — sinon un client peut usurper n'importe quelle IP.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.IndexByte(xff, ','); idx >= 0 {
				xff = xff[:idx]
			}
			if ip := strings.TrimSpace(xff); ip != "" {
				return ip
			}
		}
		if rip := r.Header.Get("X-Real-IP"); rip != "" {
			return strings.TrimSpace(rip)
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// httpRateLimitMiddleware impose une limite de débit par IP. Renvoie 429 si
// le token bucket est épuisé.
func httpRateLimitMiddleware(rl *ipRateLimiter, trustProxy bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, trustProxy)
		if !rl.allow(ip) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
