package security

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	global     *rate.Limiter
	perBuiltin map[string]*rate.Limiter
	mu         sync.RWMutex
}

func NewRateLimiter(cfg *PolicyConfig) *RateLimiter {
	rl := &RateLimiter{
		perBuiltin: make(map[string]*rate.Limiter),
	}

	if cfg.RateLimits.Global.Count > 0 {
		r := rate.Every(cfg.RateLimits.Global.Window / time.Duration(cfg.RateLimits.Global.Count))
		rl.global = rate.NewLimiter(r, cfg.RateLimits.Global.Count)
	}

	for builtinName, spec := range cfg.RateLimits.PerBuiltin {
		if spec.Count > 0 {
			r := rate.Every(spec.Window / time.Duration(spec.Count))
			rl.perBuiltin[builtinName] = rate.NewLimiter(r, spec.Count)
		}
	}

	return rl
}

func (r *RateLimiter) Allow(builtinName string) bool {
	if r.global != nil && !r.global.Allow() {
		return false
	}
	r.mu.RLock()
	lim, ok := r.perBuiltin[builtinName]
	r.mu.RUnlock()
	if ok && !lim.Allow() {
		return false
	}
	return true
}
