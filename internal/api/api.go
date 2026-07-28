// Package api serves the read-only holiday HTTP endpoints.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"
	"github.com/layhak/khmer-holiday-api/internal/store"
)

// Config tunes the public-facing behaviour of the server.
type Config struct {
	// RatePerMinute caps requests per client IP. Zero disables limiting.
	RatePerMinute int

	// RateBurst is the largest momentary burst a client may make.
	RateBurst int

	// TrustProxyHeaders makes the limiter read CF-Connecting-IP /
	// X-Forwarded-For. Enable ONLY behind a proxy or CDN: these headers are
	// client-supplied and trivially spoofed when the service is exposed
	// directly, which would let anyone evade the limit.
	TrustProxyHeaders bool

	// CacheMaxAge sets Cache-Control on holiday responses. The data changes at
	// most a few times a year, so a long TTL lets a CDN absorb the traffic.
	CacheMaxAge time.Duration
}

// Validate rejects values that would accidentally disable protection or
// overflow response headers when supplied through environment variables.
func (c Config) Validate() error {
	if c.RatePerMinute < 0 || c.RatePerMinute > 1_000_000 {
		return fmt.Errorf("rate limit must be between 0 and 1000000 requests/minute")
	}
	if c.RatePerMinute > 0 && (c.RateBurst <= 0 || c.RateBurst > 1_000_000) {
		return fmt.Errorf("rate burst must be between 1 and 1000000 when rate limiting is enabled")
	}
	if c.CacheMaxAge < 0 || c.CacheMaxAge > 365*24*time.Hour {
		return fmt.Errorf("cache max-age must be between 0 and 365 days")
	}
	return nil
}

// DefaultConfig is the configuration used when none is supplied.
func DefaultConfig() Config {
	return Config{
		RatePerMinute: 60,
		RateBurst:     20,
		CacheMaxAge:   time.Hour,
	}
}

// Server wires the routes to the store.
type Server struct {
	st      *store.Store
	mux     *http.ServeMux
	cfg     Config
	limiter *RateLimiter
}

// New builds a Server with the default configuration.
func New(st *store.Store) *Server { return NewWithConfig(st, DefaultConfig()) }

// NewWithConfig builds a Server with all routes registered.
func NewWithConfig(st *store.Store, cfg Config) *Server {
	s := &Server{
		st:      st,
		mux:     http.NewServeMux(),
		cfg:     cfg,
		limiter: NewRateLimiter(cfg.RatePerMinute, cfg.RateBurst),
	}

	s.mux.HandleFunc("GET /api/v1/holidays", s.handleList)
	s.mux.HandleFunc("GET /api/v1/holidays/{date}", s.handleByDate)
	s.mux.HandleFunc("GET /api/v1/years", s.handleYears)
	s.mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/v1/sources", s.handleSources)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /assets/site.js", s.handleSiteJS)
	s.mux.HandleFunc("GET /support/aba-khqr.png", s.handleDonationQR)
	s.mux.HandleFunc("GET /robots.txt", s.handleRobots)
	s.mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
	s.mux.HandleFunc("GET /", s.handleIndex)

	return s
}

// ServeHTTP applies CORS, caching and rate limiting, then dispatches. The
// dataset is public reference data, so cross-origin reads are allowed from
// anywhere.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setPublicHeaders(w.Header())
	w.Header().Set("Cache-Control", "no-store")
	s.limiter.Middleware(http.HandlerFunc(s.serve), s.cfg.TrustProxyHeaders).ServeHTTP(w, r)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	}

	// Only successful reference responses are cacheable. In particular, a CDN
	// must never retain a transient database failure or a caller's 400 error.
	cw := &cacheResponseWriter{
		ResponseWriter: w,
		public:         cacheable(r.URL.Path) && s.cfg.CacheMaxAge > 0,
		maxAge:         s.cfg.CacheMaxAge,
	}
	s.mux.ServeHTTP(cw, r)
}

func setPublicHeaders(h http.Header) {
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	h.Set("Access-Control-Max-Age", "86400")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	h.Set("Content-Security-Policy",
		"default-src 'none'; connect-src 'self'; img-src 'self'; script-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
}

type cacheResponseWriter struct {
	http.ResponseWriter
	public      bool
	maxAge      time.Duration
	wroteHeader bool
}

func (w *cacheResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	if w.public && code >= 200 && code < 300 {
		w.Header().Set("Cache-Control",
			fmt.Sprintf("public, max-age=%d", int(w.maxAge.Seconds())))
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *cacheResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

// cacheable reports whether a path serves slow-moving reference data.
func cacheable(path string) bool {
	switch {
	case path == "/api/v1/status", path == "/healthz":
		return false
	case strings.HasPrefix(path, "/api/v1/"), path == "/",
		path == "/assets/site.js", path == "/support/aba-khqr.png",
		path == "/robots.txt", path == "/sitemap.xml":
		return true
	}
	return false
}

type listResponse struct {
	Count    int             `json:"count"`
	Filter   map[string]any  `json:"filter,omitempty"`
	Warnings []string        `json:"warnings,omitempty"`
	Holidays []model.Holiday `json:"holidays"`
}

// handleList serves GET /api/v1/holidays with day/month/year filtering.
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	f := store.Filter{}
	applied := map[string]any{}

	for _, spec := range []struct {
		param string
		dst   *int
	}{
		{"year", &f.Year}, {"month", &f.Month}, {"day", &f.Day},
	} {
		raw := strings.TrimSpace(q.Get(spec.param))
		if raw == "" {
			continue
		}
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("%s must be an integer, got %q", spec.param, raw))
			return
		}
		switch spec.param {
		case "year":
			if v < 1900 || v > 2200 {
				writeError(w, http.StatusBadRequest, "year must be between 1900 and 2200")
				return
			}
		case "month":
			if v < 1 || v > 12 {
				writeError(w, http.StatusBadRequest, "month must be between 1 and 12")
				return
			}
		case "day":
			if v < 1 || v > 31 {
				writeError(w, http.StatusBadRequest, "day must be between 1 and 31")
				return
			}
		}
		*spec.dst = v
		applied[spec.param] = v
	}

	if k := strings.TrimSpace(q.Get("key")); k != "" {
		if !validHolidayKey(k) {
			writeError(w, http.StatusBadRequest,
				"key must contain only lowercase letters, digits, and underscores (maximum 100 characters)")
			return
		}
		f.Key = k
		applied["key"] = k
	}
	if raw := strings.TrimSpace(q.Get("official")); raw != "" {
		official, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "official must be true or false")
			return
		}
		f.OfficialOnly = official
		applied["official"] = official
	}

	for _, spec := range []struct {
		param string
		dst   *time.Time
	}{
		{"from", &f.From}, {"to", &f.To},
	} {
		raw := strings.TrimSpace(q.Get(spec.param))
		if raw == "" {
			continue
		}
		t, err := time.Parse(model.DateLayout, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("%s must be YYYY-MM-DD, got %q", spec.param, raw))
			return
		}
		if t.Year() < 1900 || t.Year() > 2200 {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("%s year must be between 1900 and 2200", spec.param))
			return
		}
		*spec.dst = t
		applied[spec.param] = raw
	}

	if err := f.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hs, err := s.st.List(r.Context(), f)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, listResponse{
		Count:    len(hs),
		Filter:   applied,
		Warnings: provisionalWarnings(hs),
		Holidays: hs,
	})
}

type dateResponse struct {
	Date      string          `json:"date"`
	IsHoliday bool            `json:"is_holiday"`
	Weekday   string          `json:"weekday"`
	Holidays  []model.Holiday `json:"holidays"`
}

// handleByDate serves GET /api/v1/holidays/2026-04-14 - a direct "is this day
// a holiday?" lookup, the query most callers actually need.
func (s *Server) handleByDate(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("date")

	d, err := time.Parse(model.DateLayout, raw)
	if err != nil {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("date must be YYYY-MM-DD, got %q", raw))
		return
	}

	hs, err := s.st.List(r.Context(), store.Filter{
		Year: d.Year(), Month: int(d.Month()), Day: d.Day(),
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, dateResponse{
		Date:      raw,
		IsHoliday: len(hs) > 0,
		Weekday:   d.Weekday().String(),
		Holidays:  hs,
	})
}

func (s *Server) handleYears(w http.ResponseWriter, r *http.Request) {
	years, err := s.st.Years(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"years": years})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.Status(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	fetches, err := s.st.Fetches(r.Context(), 0)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for i := range fetches {
		if !fetches[i].OK {
			// Failure details can contain local command paths or untrusted
			// helper stderr. Keep them in the operator's database/CLI, but do
			// not disclose them through the unauthenticated public endpoint.
			fetches[i].Note = "source unavailable; inspect scraper logs for details"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"years":   st,
		"fetches": fetches,
	})
}

// sourceInfo documents each upstream and its current usability.
type sourceInfo struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Authority string `json:"authority"`
	Status    string `json:"status"`
	Notes     string `json:"notes"`
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": []sourceInfo{
		{"tallyfy", "https://tallyfy.com/national-holidays/api/KH/{year}.json", "computed", "working",
			"Public JSON calendar for 2026-2030. Lowest-precedence cross-check; bank-only closures are excluded and it cannot authorize replacement."},
		{"nager", "https://date.nager.at/api/v3/PublicHolidays/{year}/KH", "computed", "working",
			"Free JSON API, no key. Supplies the dates, including projections for future years."},
		{"wikipedia", "https://en.wikipedia.org/wiki/Public_holidays_in_Cambodia", "computed", "working",
			"Fixed-date holidays and Khmer names via the MediaWiki API. Emits no lunar dates by design."},
		{"akp", "https://akp.gov.kh", "reported", "working",
			"State news agency. Announces the sub-decree and its total day count, used to corroborate."},
		{"mlvt", "https://www.mlvt.gov.kh", "official", "working (evidence only)",
			"Ministry of Labour publishes the annual paid-holiday Prakas. PDF is a scanned image with no text layer, so dates need OCR or manual verification."},
		{"mlvt_verified", "https://www.mlvt.gov.kh", "official", "working",
			"Past calendars manually transcribed and visually verified against signed MLVT Prakas documents."},
		{"nbc", "https://www.nbc.gov.kh/english/news_and_events/official_holiday.php", "official", "working",
			"National Bank of Cambodia publishes the current year's official public-holiday dates as a machine-readable table."},
		{"mef", "https://mef.gov.kh", "official", "blocked",
			"Returns HTTP 403 to non-browser clients (Cloudflare). Set MEF_FETCH_CMD to a headless fetcher to enable."},
	}})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if _, err := s.st.Years(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// provisionalWarnings surfaces unconfirmed dates in the response body so a
// caller cannot mistake a projection for a confirmed date without opting in.
func provisionalWarnings(hs []model.Holiday) []string {
	byYear := map[int]int{}
	for _, h := range hs {
		if h.Conf != model.ConfidenceOfficial {
			byYear[h.Year()]++
		}
	}
	if len(byYear) == 0 {
		return nil
	}
	out := []string{}
	years := make([]int, 0, len(byYear))
	for y := range byYear {
		years = append(years, y)
	}
	sort.Ints(years)
	for _, y := range years {
		n := byYear[y]
		out = append(out, fmt.Sprintf(
			"%d of the returned %d holidays are not yet confirmed against the official sub-decree; "+
				"lunar dates may shift. Filter with ?official=true to exclude them.", n, y))
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		// Header is already sent; nothing useful left to do but stop.
		return
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg, "status": code})
}

func writeInternalError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("request %s %q failed: %v", r.Method, r.URL.Path, err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func validHolidayKey(key string) bool {
	if key == "" || len(key) > 100 {
		return false
	}
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// Run starts the server and shuts it down cleanly when ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
