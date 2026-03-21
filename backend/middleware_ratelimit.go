package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimitEntry tracks request tokens for a single IP+endpoint key.
type rateLimitEntry struct {
	tokens    float64
	lastCheck time.Time
}

// RateLimiter enforces per-IP+endpoint request limits using a token bucket.
type RateLimiter struct {
	limits  map[string]float64 // endpoint → requests per minute
	entries sync.Map           // key (ip|path) → *rateLimitEntry
	mu      sync.Map           // per-key mutex to avoid races on token updates
	ttl     time.Duration
	stop    chan struct{}
}

// NewRateLimiter creates a rate limiter with the given per-endpoint limits
// (requests per minute). It starts a background goroutine that purges stale
// entries every minute.
func NewRateLimiter(limits map[string]float64, ttl time.Duration) *RateLimiter {
	rl := &RateLimiter{
		limits: limits,
		ttl:    ttl,
		stop:   make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Stop halts the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stop)
}

// cleanup removes entries older than ttl every minute.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			now := time.Now()
			rl.entries.Range(func(key, value any) bool {
				e := value.(*rateLimitEntry)
				if now.Sub(e.lastCheck) > rl.ttl {
					rl.entries.Delete(key)
					rl.mu.Delete(key)
				}
				return true
			})
		}
	}
}

// allow checks whether a request from ip to path is allowed.
// Returns (allowed, secondsUntilNextToken).
func (rl *RateLimiter) allow(ip, path string) (bool, int) {
	limit, ok := rl.limits[path]
	if !ok {
		return true, 0
	}

	key := ip + "|" + path

	// Per-key mutex so concurrent requests for the same key are serialized.
	muIface, _ := rl.mu.LoadOrStore(key, &sync.Mutex{})
	mu := muIface.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	now := time.Now()
	rate := limit / 60.0 // tokens per second

	eIface, loaded := rl.entries.LoadOrStore(key, &rateLimitEntry{
		tokens:    limit - 1, // consume one token immediately
		lastCheck: now,
	})
	if !loaded {
		return true, 0
	}

	e := eIface.(*rateLimitEntry)
	elapsed := now.Sub(e.lastCheck).Seconds()
	e.tokens += elapsed * rate
	if e.tokens > limit {
		e.tokens = limit
	}
	e.lastCheck = now

	if e.tokens < 1 {
		retryAfter := int((1-e.tokens)/rate) + 1
		return false, retryAfter
	}

	e.tokens--
	return true, 0
}

// clientIP extracts the client IP, preferring X-Forwarded-For.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First entry is the original client.
		ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		if ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Middleware returns an http.Handler that enforces rate limits on configured
// endpoints and passes all other requests through unchanged.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip tenant path prefix (e.g. /t/slug/login → /login).
		path := r.URL.Path
		if strings.HasPrefix(path, "/t/") {
			if idx := strings.Index(path[3:], "/"); idx >= 0 {
				path = path[3+idx:]
			}
		}

		if _, isLimited := rl.limits[path]; isLimited {
			ip := clientIP(r)
			allowed, retryAfter := rl.allow(ip, path)
			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error":             "too_many_requests",
					"error_description": "rate limit exceeded",
				})
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
