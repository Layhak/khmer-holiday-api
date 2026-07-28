package api

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter is a per-IP token bucket.
//
// This is a public, unauthenticated API, so a single crawler can otherwise
// exhaust the box. Buckets refill continuously rather than resetting on a fixed
// window, which avoids the burst-at-the-boundary problem a fixed window has.
//
// State is in-process: it is per-instance, not shared across replicas. That is
// the right trade for a single small service - a distributed limiter would mean
// running Redis to protect a database that is a single file on disk. Put a CDN
// in front and most traffic never reaches this code at all.
type RateLimiter struct {
	rate  float64 // tokens added per second
	burst float64 // maximum tokens held

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter returns a limiter allowing perMinute requests per IP with the
// given burst. A perMinute of zero disables limiting entirely.
func NewRateLimiter(perMinute, burst int) *RateLimiter {
	if perMinute <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = perMinute
	}
	rl := &RateLimiter{
		rate:    float64(perMinute) / 60.0,
		burst:   float64(burst),
		buckets: make(map[string]*bucket),
	}
	go rl.reap()
	return rl
}

// Allow reports whether a request from key may proceed, and how long to wait if
// not.
func (rl *RateLimiter) Allow(key string) (bool, time.Duration) {
	if rl == nil {
		return true, 0
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, last: now}
		return true, 0
	}

	// Refill for the time elapsed since this bucket was last touched.
	b.tokens += now.Sub(b.last).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now

	if b.tokens < 1 {
		// Time until one whole token is available again.
		wait := time.Duration((1 - b.tokens) / rl.rate * float64(time.Second))
		return false, wait
	}

	b.tokens--
	return true, 0
}

// reap drops buckets that have sat idle long enough to be fully refilled, so
// the map does not grow without bound as IPs come and go.
func (rl *RateLimiter) reap() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-10 * time.Minute)

		rl.mu.Lock()
		for k, b := range rl.buckets {
			if b.last.Before(cutoff) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware enforces the limit, keyed by client IP.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	if rl == nil {
		return next
	}
	limit := strconv.Itoa(int(rl.rate * 60))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health checks must never be rate limited or an orchestrator will
		// start killing healthy containers under load.
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		key := clientIP(r)
		w.Header().Set("X-RateLimit-Limit", limit)

		ok, wait := rl.Allow(key)
		if !ok {
			retry := int(wait.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf(
				"rate limit exceeded (%s requests/minute per IP); retry in %ds",
				limit, retry))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// trustedProxyHeaders controls whether forwarded headers are believed. They are
// trivially spoofable when the service is exposed directly, so honouring them
// is opt-in for deployments that actually sit behind a proxy or CDN.
var trustedProxyHeaders bool

// clientIP resolves the caller's address.
func clientIP(r *http.Request) string {
	if trustedProxyHeaders {
		// Cloudflare's header is the most specific, so prefer it.
		if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
			return ip
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Left-most entry is the original client.
			if first, _, found := strings.Cut(xff, ","); found {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(xff)
		}
		if ip := r.Header.Get("X-Real-IP"); ip != "" {
			return ip
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
