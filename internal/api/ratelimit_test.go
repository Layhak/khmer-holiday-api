package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsBurstThenBlocks(t *testing.T) {
	rl := NewRateLimiter(60, 5) // 1/sec, burst 5

	for i := range 5 {
		if ok, _ := rl.Allow("1.2.3.4"); !ok {
			t.Fatalf("request %d within burst was blocked", i+1)
		}
	}
	ok, wait := rl.Allow("1.2.3.4")
	if ok {
		t.Error("request beyond burst was allowed, want blocked")
	}
	if wait <= 0 {
		t.Errorf("wait = %v, want a positive retry delay", wait)
	}
}

func TestRateLimiterIsolatesClients(t *testing.T) {
	rl := NewRateLimiter(60, 2)

	rl.Allow("1.1.1.1")
	rl.Allow("1.1.1.1")
	if ok, _ := rl.Allow("1.1.1.1"); ok {
		t.Fatal("first client should be exhausted")
	}
	if ok, _ := rl.Allow("2.2.2.2"); !ok {
		t.Error("second client was blocked by the first client's usage")
	}
}

func TestRateLimiterRefills(t *testing.T) {
	rl := NewRateLimiter(6000, 1) // 100/sec: one token back every 10ms

	if ok, _ := rl.Allow("ip"); !ok {
		t.Fatal("first request blocked")
	}
	if ok, _ := rl.Allow("ip"); ok {
		t.Fatal("second immediate request should be blocked")
	}

	time.Sleep(30 * time.Millisecond)

	if ok, _ := rl.Allow("ip"); !ok {
		t.Error("bucket did not refill after waiting")
	}
}

func TestNilRateLimiterAllowsEverything(t *testing.T) {
	var rl *RateLimiter // NewRateLimiter(0, …) returns nil

	if ok, _ := rl.Allow("ip"); !ok {
		t.Error("a disabled limiter must allow all requests")
	}

	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 when limiting is disabled", rec.Code)
	}
}

func TestMiddlewareReturns429WithRetryAfter(t *testing.T) {
	rl := NewRateLimiter(60, 1)
	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), false)

	req := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/holidays", nil)
		r.RemoteAddr = "9.9.9.9:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	if got := req().Code; got != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", got)
	}

	rec := req()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response is missing Retry-After")
	}
	if rec.Header().Get("X-RateLimit-Limit") != "60" {
		t.Errorf("X-RateLimit-Limit = %q, want 60", rec.Header().Get("X-RateLimit-Limit"))
	}
}

// An orchestrator's health probe must never be throttled, or it will start
// killing containers that are merely busy.
func TestHealthzBypassesRateLimit(t *testing.T) {
	rl := NewRateLimiter(60, 1)
	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), false)

	for i := range 5 {
		r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		r.RemoteAddr = "8.8.8.8:1"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("healthz probe %d got %d, want 200", i+1, rec.Code)
		}
	}
}

// Forwarded headers are spoofable, so they must be ignored unless the operator
// has declared the service sits behind a trusted proxy.
func TestClientIPIgnoresProxyHeadersByDefault(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := clientIP(r, false); got != "10.0.0.1" {
		t.Errorf("clientIP = %q, want the socket address 10.0.0.1 when proxies are untrusted", got)
	}
}

func TestClientIPUsesProxyHeadersWhenTrusted(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.9")

	if got := clientIP(r, true); got != "1.2.3.4" {
		t.Errorf("clientIP = %q, want the left-most forwarded address 1.2.3.4", got)
	}

	r.Header.Set("CF-Connecting-IP", "5.6.7.8")
	if got := clientIP(r, true); got != "5.6.7.8" {
		t.Errorf("clientIP = %q, want CF-Connecting-IP to take priority", got)
	}
}

func TestClientIPRejectsMalformedProxyHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("CF-Connecting-IP", "attacker-controlled-value")
	r.Header.Set("X-Forwarded-For", "also-not-an-ip")

	if got := clientIP(r, true); got != "10.0.0.1" {
		t.Errorf("clientIP = %q, want socket fallback for malformed proxy headers", got)
	}
}

func TestRateLimiterBoundsIdentityMap(t *testing.T) {
	rl := NewRateLimiter(60, 1)
	rl.maxBuckets = 2

	rl.Allow("1.1.1.1")
	rl.Allow("2.2.2.2")
	rl.Allow("3.3.3.3")
	rl.Allow("4.4.4.4")

	if len(rl.buckets) != 3 { // two identities plus one shared overflow bucket
		t.Fatalf("bucket count = %d, want 3 bounded buckets", len(rl.buckets))
	}
	if _, ok := rl.buckets["3.3.3.3"]; ok {
		t.Error("new identities should not allocate dedicated buckets after the cap")
	}
}

func TestCacheablePaths(t *testing.T) {
	cases := map[string]bool{
		"/api/v1/holidays":            true,
		"/api/v1/holidays/2026-04-14": true,
		"/api/v1/years":               true,
		"/":                           true,
		"/api/v1/status":              false, // operators need fresh scrape results
		"/healthz":                    false,
	}
	for path, want := range cases {
		if got := cacheable(path); got != want {
			t.Errorf("cacheable(%q) = %v, want %v", path, got, want)
		}
	}
}
