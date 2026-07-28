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
// State is in-process because production runs one API instance. It deliberately
// does not depend on the optional Redis response cache, so a cache outage cannot
// disable request protection.
type RateLimiter struct {
	rate        float64 // tokens added per second
	burst       float64 // maximum tokens held
	penaltyBase time.Duration
	penaltyMax  time.Duration
	now         func() time.Time

	mu         sync.Mutex
	buckets    map[string]*bucket
	maxBuckets int
}

type bucket struct {
	tokens        float64
	last          time.Time
	penalty       time.Duration
	blockedUntil  time.Time
	lastViolation time.Time
}

const (
	defaultMaxBuckets       = 10_000
	overflowBucketKey       = "<overflow>"
	defaultRatePenaltyBase  = 5 * time.Second
	defaultRatePenaltyMax   = 15 * time.Minute
	ratePenaltyResetAfter   = 10 * time.Minute
	rateBucketIdleRetention = 30 * time.Minute
)

// NewRateLimiter returns a limiter allowing perMinute requests per IP with the
// given burst. A perMinute of zero disables limiting entirely.
func NewRateLimiter(perMinute, burst int) *RateLimiter {
	return NewRateLimiterWithPenalty(
		perMinute,
		burst,
		defaultRatePenaltyBase,
		defaultRatePenaltyMax,
	)
}

// NewRateLimiterWithPenalty adds an escalating cooldown for clients that keep
// requesting during a 429 response. A client that respects Retry-After can
// resume normally; a spammer is delayed exponentially up to penaltyMax.
func NewRateLimiterWithPenalty(
	perMinute, burst int,
	penaltyBase, penaltyMax time.Duration,
) *RateLimiter {
	if perMinute <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = perMinute
	}
	if penaltyBase <= 0 {
		penaltyBase = defaultRatePenaltyBase
	}
	if penaltyMax < penaltyBase {
		penaltyMax = penaltyBase
	}
	rl := &RateLimiter{
		rate:        float64(perMinute) / 60.0,
		burst:       float64(burst),
		penaltyBase: penaltyBase,
		penaltyMax:  penaltyMax,
		now:         time.Now,
		buckets:     make(map[string]*bucket),
		maxBuckets:  defaultMaxBuckets,
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

	now := rl.now()
	b, ok := rl.buckets[key]
	if !ok && len(rl.buckets) >= rl.maxBuckets {
		// Bound memory even when a botnet (or a misconfigured trusted proxy)
		// presents an unending stream of new addresses. New identities share
		// a fail-closed overflow bucket until idle entries are reaped.
		key = overflowBucketKey
		b, ok = rl.buckets[key]
	}
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

	if !b.lastViolation.IsZero() && !now.Before(b.blockedUntil) &&
		now.Sub(b.lastViolation) >= ratePenaltyResetAfter {
		b.penalty = 0
		b.blockedUntil = time.Time{}
		b.lastViolation = time.Time{}
	}

	if now.Before(b.blockedUntil) {
		b.penalty = rl.nextPenalty(b.penalty)
		b.blockedUntil = now.Add(b.penalty)
		b.lastViolation = now
		return false, b.penalty
	}

	if b.tokens < 1 {
		b.penalty = rl.nextPenalty(b.penalty)
		b.blockedUntil = now.Add(b.penalty)
		b.lastViolation = now
		return false, b.penalty
	}

	b.tokens--
	return true, 0
}

func (rl *RateLimiter) nextPenalty(current time.Duration) time.Duration {
	if current < rl.penaltyBase {
		return rl.penaltyBase
	}
	if current >= rl.penaltyMax/2 {
		return rl.penaltyMax
	}
	return current * 2
}

// reap drops buckets that have sat idle long enough to be fully refilled, so
// the map does not grow without bound as IPs come and go.
func (rl *RateLimiter) reap() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-rateBucketIdleRetention)

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
func (rl *RateLimiter) Middleware(next http.Handler, trustProxyHeaders bool) http.Handler {
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

		key := clientIP(r, trustProxyHeaders)
		w.Header().Set("X-RateLimit-Limit", limit)

		ok, wait := rl.Allow(key)
		if !ok {
			retry := int((wait + time.Second - 1) / time.Second)
			if retry < 1 {
				retry = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf(
				"rate limit exceeded (%s requests/minute per IP); "+
					"repeated requests extend the cooldown; retry in %ds",
				limit, retry))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP resolves the caller's address.
func clientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		// Cloudflare's header is the most specific, so prefer it.
		if ip := validIP(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Left-most entry is the original client.
			if first, _, found := strings.Cut(xff, ","); found {
				if ip := validIP(first); ip != "" {
					return ip
				}
			} else if ip := validIP(xff); ip != "" {
				return ip
			}
		}
		if ip := validIP(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if ip := validIP(r.RemoteAddr); ip != "" {
			return ip
		}
		return "<unknown>"
	}
	if ip := validIP(host); ip != "" {
		return ip
	}
	return "<unknown>"
}

func validIP(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return ""
	}
	return ip.String()
}
