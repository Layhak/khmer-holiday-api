package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
)

type fakeResponseCache struct {
	mu      sync.Mutex
	values  map[string][]byte
	getErr  error
	gets    int
	sets    int
	lastTTL time.Duration
}

func newFakeResponseCache() *fakeResponseCache {
	return &fakeResponseCache{values: map[string][]byte{}}
}

func (c *fakeResponseCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets++
	if c.getErr != nil {
		return nil, false, c.getErr
	}
	value, ok := c.values[key]
	return append([]byte(nil), value...), ok, nil
}

func (c *fakeResponseCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sets++
	c.lastTTL = ttl
	c.values[key] = append([]byte(nil), value...)
	return nil
}

func TestRedisResponseCacheMissThenHit(t *testing.T) {
	_, st := testServer(t)
	cache := newFakeResponseCache()
	cfg := DefaultConfig()
	cfg.RatePerMinute = 0
	cfg.ResponseCacheTTL = 3 * time.Minute
	s := NewWithConfigAndCache(st, cfg, cache)

	first := perform(s, "/api/v1/holidays?year=2027")
	if first.Code != http.StatusOK || first.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("first response: status=%d cache=%q", first.Code, first.Header().Get("X-Cache"))
	}
	if cache.sets != 1 || cache.lastTTL != 3*time.Minute {
		t.Fatalf("cache writes=%d ttl=%s, want 1 and 3m", cache.sets, cache.lastTTL)
	}

	// Prove the second response is served from Redis rather than SQLite.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	second := perform(s, "/api/v1/holidays?year=2027")
	if second.Code != http.StatusOK || second.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("second response: status=%d cache=%q", second.Code, second.Header().Get("X-Cache"))
	}
	if second.Body.String() != first.Body.String() {
		t.Fatal("cached response body changed")
	}
}

func TestRedisCacheKeyNormalizesQueryOrder(t *testing.T) {
	a := &http.Request{URL: &url.URL{
		Path:     "/api/v1/holidays",
		RawQuery: "year=2027&month=1",
	}}
	b := &http.Request{URL: &url.URL{
		Path:     "/api/v1/holidays",
		RawQuery: "month=1&year=2027",
	}}
	if responseCacheKey(a) != responseCacheKey(b) {
		t.Fatal("equivalent query strings produced different Redis keys")
	}
}

func TestRedisCacheRejectsUnknownAndDuplicateQueryParameters(t *testing.T) {
	for _, rawURL := range []string{
		"/api/v1/holidays?year=2027&random=value",
		"/api/v1/holidays?year=2027&year=2026",
		"/api/v1/years?random=value",
		"/api/v1/holidays/2027-01-01?random=value",
	} {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if redisResponseCacheable(req) {
			t.Errorf("%q was accepted for Redis caching", rawURL)
		}
	}
}

func TestRedisFailureFallsBackWithoutCachingErrorResponses(t *testing.T) {
	_, st := testServer(t)
	cache := newFakeResponseCache()
	cache.getErr = errors.New("redis unavailable")
	cfg := DefaultConfig()
	cfg.RatePerMinute = 0
	s := NewWithConfigAndCache(st, cfg, cache)

	ok := perform(s, "/api/v1/holidays?year=2027")
	if ok.Code != http.StatusOK || ok.Header().Get("X-Cache") != "BYPASS" {
		t.Fatalf("fallback response: status=%d cache=%q", ok.Code, ok.Header().Get("X-Cache"))
	}
	if cache.sets != 0 {
		t.Fatalf("cache writes after Redis failure = %d, want 0", cache.sets)
	}

	cache.getErr = nil
	missing := perform(s, "/api/v1/holidays?key=does_not_exist")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown-key status = %d, want 404", missing.Code)
	}
	if cache.sets != 0 {
		t.Fatalf("cache writes after 404 = %d, want 0", cache.sets)
	}
}
