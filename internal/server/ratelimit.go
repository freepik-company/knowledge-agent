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
	limiters       map[string]*rate.Limiter
	mu             sync.RWMutex
	rate           rate.Limit
	burst          int
	cleanup        *time.Ticker
	trustedProxies []*net.IPNet // Parsed CIDR ranges for trusted proxies
}

// NewRateLimiter creates a new rate limiter
// requestsPerSecond: allowed requests per second per IP
// burst: maximum burst size (tokens that can accumulate)
// trustedProxies: list of trusted proxy IPs/CIDRs (X-Forwarded-For is only used if request comes from these)
func NewRateLimiter(requestsPerSecond float64, burst int, trustedProxies []string) *RateLimiter {
	rl := &RateLimiter{
		limiters:       make(map[string]*rate.Limiter),
		rate:           rate.Limit(requestsPerSecond),
		burst:          burst,
		cleanup:        time.NewTicker(5 * time.Minute), // Cleanup old entries every 5 minutes
		trustedProxies: parseTrustedProxies(trustedProxies),
	}

	// Start cleanup goroutine
	go rl.cleanupRoutine()

	return rl
}

// parseTrustedProxies parses a list of IP addresses and CIDRs into net.IPNet
func parseTrustedProxies(proxies []string) []*net.IPNet {
	var result []*net.IPNet
	log := logger.Get()

	for _, proxy := range proxies {
		// Try to parse as CIDR first
		_, ipNet, err := net.ParseCIDR(proxy)
		if err == nil {
			result = append(result, ipNet)
			continue
		}

		// Try to parse as single IP
		ip := net.ParseIP(proxy)
		if ip != nil {
			// Convert single IP to /32 (IPv4) or /128 (IPv6)
			var mask net.IPMask
			if ip.To4() != nil {
				mask = net.CIDRMask(32, 32)
			} else {
				mask = net.CIDRMask(128, 128)
			}
			result = append(result, &net.IPNet{IP: ip, Mask: mask})
			continue
		}

		log.Warnw("Invalid trusted proxy format (ignored)",
			"proxy", proxy,
			"hint", "Use IP address (1.2.3.4) or CIDR notation (10.0.0.0/8)",
		)
	}

	if len(result) > 0 {
		log.Infow("Trusted proxies configured for rate limiting",
			"count", len(result),
		)
	}

	return result
}

// isTrustedProxy checks if the given IP is in the trusted proxy list
func (rl *RateLimiter) isTrustedProxy(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	for _, trusted := range rl.trustedProxies {
		if trusted.Contains(parsedIP) {
			return true
		}
	}
	return false
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

			// Extract direct connection IP address
			directIP, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				// Fallback to full RemoteAddr if split fails
				directIP = r.RemoteAddr
			}

			// Default to direct IP
			clientIP := directIP

			// Only trust X-Forwarded-For if the direct connection is from a trusted proxy
			// This prevents attackers from spoofing X-Forwarded-For headers
			if len(rl.trustedProxies) > 0 && rl.isTrustedProxy(directIP) {
				if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
					// X-Forwarded-For format: "client, proxy1, proxy2"
					// When behind a trusted proxy, take the first IP (original client)
					// The trusted proxy should have added the real client IP at the start
					for i := 0; i < len(forwarded); i++ {
						if forwarded[i] == ',' {
							clientIP = forwarded[:i]
							break
						}
					}
					if clientIP == directIP {
						// No comma found, use the whole header
						clientIP = forwarded
					}
					// Trim spaces
					for len(clientIP) > 0 && clientIP[0] == ' ' {
						clientIP = clientIP[1:]
					}
					for len(clientIP) > 0 && clientIP[len(clientIP)-1] == ' ' {
						clientIP = clientIP[:len(clientIP)-1]
					}
					// Remove port if present
					if ip, _, err := net.SplitHostPort(clientIP); err == nil {
						clientIP = ip
					}
				}
			}

			limiter := rl.getLimiter(clientIP)

			if !limiter.Allow() {
				log.Warnw("Rate limit exceeded",
					"client_ip", clientIP,
					"direct_ip", directIP,
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
