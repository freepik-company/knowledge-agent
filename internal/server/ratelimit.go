package server

import (
	"net"
	"net/http"
	"sync"
	"time"

	"knowledge-agent/internal/logger"

	"golang.org/x/time/rate"
)

// RateLimiter implements a per-IP rate limiter using token bucket algorithm
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
	cleanup  *time.Ticker
}

// NewRateLimiter creates a new rate limiter
// requestsPerSecond: allowed requests per second per IP
// burst: maximum burst size (tokens that can accumulate)
func NewRateLimiter(requestsPerSecond float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(requestsPerSecond),
		burst:    burst,
		cleanup:  time.NewTicker(5 * time.Minute), // Cleanup old entries every 5 minutes
	}

	// Start cleanup goroutine
	go rl.cleanupRoutine()

	return rl
}

// getLimiter returns the rate limiter for a given IP address
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(rl.rate, rl.burst)
		rl.limiters[ip] = limiter
	}

	return limiter
}

// cleanupRoutine periodically removes inactive limiters
func (rl *RateLimiter) cleanupRoutine() {
	for range rl.cleanup.C {
		rl.mu.Lock()
		// Simple cleanup: remove all limiters (they'll be recreated on next request)
		// More sophisticated approach would track last access time
		rl.limiters = make(map[string]*rate.Limiter)
		rl.mu.Unlock()
	}
}

// Close stops the cleanup goroutine
func (rl *RateLimiter) Close() {
	rl.cleanup.Stop()
}

// Middleware returns an HTTP middleware that applies rate limiting
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.Get()

			// Extract IP address
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				// Fallback to full RemoteAddr if split fails
				ip = r.RemoteAddr
			}

			// Check for forwarded IP (useful when behind proxy/load balancer)
			if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				// Take the first IP in the list
				if forwardedIP, _, err := net.SplitHostPort(forwarded); err == nil {
					ip = forwardedIP
				} else {
					ip = forwarded
				}
			}

			limiter := rl.getLimiter(ip)

			if !limiter.Allow() {
				log.Warnw("Rate limit exceeded",
					"ip", ip,
					"path", r.URL.Path,
					"method", r.Method,
				)
				http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
