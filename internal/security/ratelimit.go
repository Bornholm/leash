package security

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter gère les limites de débit par skill et globalement.
type RateLimiter struct {
	global   *rate.Limiter
	perSkill map[string]*rate.Limiter
	mu       sync.RWMutex
}

// NewRateLimiter crée un RateLimiter depuis la configuration de politique.
func NewRateLimiter(cfg *PolicyConfig) *RateLimiter {
	rl := &RateLimiter{
		perSkill: make(map[string]*rate.Limiter),
	}

	if cfg.RateLimits.Global.Count > 0 {
		r := rate.Every(cfg.RateLimits.Global.Window / time.Duration(cfg.RateLimits.Global.Count))
		rl.global = rate.NewLimiter(r, cfg.RateLimits.Global.Count)
	}

	for skillName, spec := range cfg.RateLimits.PerSkill {
		if spec.Count > 0 {
			r := rate.Every(spec.Window / time.Duration(spec.Count))
			rl.perSkill[skillName] = rate.NewLimiter(r, spec.Count)
		}
	}

	return rl
}

// Allow retourne vrai si l'appel au skill est autorisé par les limites de débit.
func (r *RateLimiter) Allow(skillName string) bool {
	if r.global != nil && !r.global.Allow() {
		return false
	}
	r.mu.RLock()
	lim, ok := r.perSkill[skillName]
	r.mu.RUnlock()
	if ok && !lim.Allow() {
		return false
	}
	return true
}
