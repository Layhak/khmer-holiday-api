// Command api serves the Cambodia public holiday HTTP API.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/api"
	"github.com/layhak/khmer-holiday-api/internal/rediscache"
	"github.com/layhak/khmer-holiday-api/internal/store"
)

func main() {
	var (
		addr   = flag.String("addr", envOr("KHAPI_ADDR", ":8080"), "listen address")
		dbPath = flag.String("db", envOr("KHAPI_DB", "data/holidays.db"), "path to the SQLite database")

		rate = flag.Int("rate", envInt("KHAPI_RATE_LIMIT", 60),
			"requests per minute per IP (0 disables rate limiting)")
		burst = flag.Int("burst", envInt("KHAPI_RATE_BURST", 20),
			"largest momentary burst per IP")
		cacheAge = flag.Int("cache", envInt("KHAPI_CACHE_SECONDS", 3600),
			"Cache-Control max-age in seconds for holiday responses")
		redisURL = flag.String("redis", envOr("KHAPI_REDIS_URL", ""),
			"Redis URL for optional server-side response caching")
		redisCacheAge = flag.Int("redis-cache", envInt("KHAPI_REDIS_CACHE_SECONDS", 300),
			"Redis response-cache TTL in seconds (0 disables)")
		trustProxy = flag.Bool("trust-proxy", envBool("KHAPI_TRUST_PROXY", false),
			"read client IP from CF-Connecting-IP/X-Forwarded-For; enable ONLY behind a proxy or CDN")
	)
	flag.Parse()

	cfg := api.Config{
		RatePerMinute:     *rate,
		RateBurst:         *burst,
		TrustProxyHeaders: *trustProxy,
		CacheMaxAge:       time.Duration(*cacheAge) * time.Second,
		ResponseCacheTTL:  time.Duration(*redisCacheAge) * time.Second,
	}

	if err := run(*addr, *dbPath, *redisURL, cfg); err != nil {
		log.Fatalf("api: %v", err)
	}
}

func run(addr, dbPath, redisURL string, cfg api.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	var responseCache api.ResponseCache
	if redisURL != "" && cfg.ResponseCacheTTL > 0 {
		cache, err := rediscache.Open(ctx, redisURL)
		if err != nil {
			log.Printf("warning: Redis cache unavailable; continuing with SQLite: %v", err)
		} else {
			responseCache = cache
			defer cache.Close()
			log.Printf("Redis response cache enabled (TTL %s)", cfg.ResponseCacheTTL)
		}
	}

	years, err := st.Years(ctx)
	if err != nil {
		return err
	}
	if len(years) == 0 {
		log.Printf("warning: database %s is empty - run `khapi-scrape --year %d` to populate it",
			dbPath, currentYear())
	} else {
		log.Printf("serving %d year(s): %v", len(years), years)
	}

	if cfg.RatePerMinute > 0 {
		log.Printf("rate limit: %d req/min per IP (burst %d, trust-proxy=%v)",
			cfg.RatePerMinute, cfg.RateBurst, cfg.TrustProxyHeaders)
	} else {
		log.Print("rate limit: disabled")
	}

	log.Printf("listening on %s (docs at http://localhost%s/)", addr, portOf(addr))
	return api.NewWithConfigAndCache(st, cfg, responseCache).Run(ctx, addr)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("warning: %s=%q is not an integer, using %d", key, v, def)
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		log.Printf("warning: %s=%q is not a boolean, using %v", key, v, def)
	}
	return def
}

func portOf(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return addr
	}
	return ""
}

func currentYear() int { return time.Now().Year() }
