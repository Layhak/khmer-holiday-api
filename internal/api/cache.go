package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const maxRedisResponseBytes = 2 << 20

// ResponseCache is the small cache-aside contract used by the HTTP layer.
// Redis implements it in production; tests use an in-memory fake.
type ResponseCache interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

func responseCacheKey(r *http.Request) string {
	target := r.URL.Path
	if query := r.URL.Query().Encode(); query != "" {
		target += "?" + query
	}
	sum := sha256.Sum256([]byte(target))
	return fmt.Sprintf("khapi:response:v1:%x", sum)
}

func redisResponseCacheable(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch {
	case r.URL.Path == "/api/v1/holidays":
		allowed := map[string]bool{
			"year": true, "month": true, "day": true, "key": true,
			"official": true, "from": true, "to": true,
		}
		for key, values := range r.URL.Query() {
			if !allowed[key] || len(values) != 1 {
				return false
			}
		}
		return true
	case strings.HasPrefix(r.URL.Path, "/api/v1/holidays/"),
		r.URL.Path == "/api/v1/years",
		r.URL.Path == "/api/v1/sources":
		return r.URL.RawQuery == ""
	}
	return false
}

func (s *Server) serveWithResponseCache(w http.ResponseWriter, r *http.Request) {
	key := responseCacheKey(r)
	value, found, err := s.responseCache.Get(r.Context(), key)
	switch {
	case err == nil && found:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(value)
		return
	case err == nil:
		w.Header().Set("X-Cache", "MISS")
	default:
		// Redis is an optimization, not a new availability dependency.
		w.Header().Set("X-Cache", "BYPASS")
	}

	capture := &cacheCapture{
		ResponseWriter: w,
		maxBytes:       maxRedisResponseBytes,
	}
	s.mux.ServeHTTP(capture, r)

	if err != nil || capture.status != http.StatusOK || capture.tooLarge ||
		!strings.HasPrefix(capture.Header().Get("Content-Type"), "application/json") {
		return
	}
	_ = s.responseCache.Set(r.Context(), key, capture.body.Bytes(), s.cfg.ResponseCacheTTL)
}

// cacheCapture forwards the response immediately while retaining a bounded
// copy for Redis. Large or unsuccessful responses are never cached.
type cacheCapture struct {
	http.ResponseWriter
	body      bytes.Buffer
	status    int
	maxBytes  int
	tooLarge  bool
	wroteHead bool
}

func (w *cacheCapture) WriteHeader(code int) {
	if w.wroteHead {
		return
	}
	w.wroteHead = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *cacheCapture) Write(p []byte) (int, error) {
	if !w.wroteHead {
		w.WriteHeader(http.StatusOK)
	}
	if !w.tooLarge {
		if w.body.Len()+len(p) <= w.maxBytes {
			_, _ = w.body.Write(p)
		} else {
			w.tooLarge = true
			w.body.Reset()
		}
	}
	return w.ResponseWriter.Write(p)
}
