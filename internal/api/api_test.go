package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"
	"github.com/layhak/khmer-holiday-api/internal/store"
)

func testServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	_, _, _, err = st.Upsert(context.Background(), []model.Holiday{{
		Date:      time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		Key:       "intl_new_year",
		NameEN:    "International New Year's Day",
		Conf:      model.ConfidenceComputed,
		Source:    "test",
		UpdatedAt: time.Now().UTC(),
	}})
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.RatePerMinute = 0
	return NewWithConfig(st, cfg), st
}

func perform(s *Server, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestSuccessfulResponsesAreCachedAndHardened(t *testing.T) {
	s, _ := testServer(t)
	rec := perform(s, "/api/v1/holidays?year=2027")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want public cache", got)
	}
	for _, header := range []string{
		"Content-Security-Policy", "Permissions-Policy", "Referrer-Policy",
		"X-Content-Type-Options", "X-Frame-Options",
	} {
		if rec.Header().Get(header) == "" {
			t.Errorf("missing security header %s", header)
		}
	}
}

func TestHomepageHasDonationAndSearchMetadata(t *testing.T) {
	s, _ := testServer(t)
	rec := perform(s, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	for _, want := range []string{
		"<title>Cambodia Public Holidays",
		`rel="canonical" href="https://khmerholiday.layhak.dev/"`,
		`property="og:image" content="https://khmerholiday.layhak.dev/assets/social-preview.png"`,
		`name="twitter:card" content="summary_large_image"`,
		`id="support"`,
		`src="/support/aba-khqr.png"`,
		`src="/assets/site.js"`,
		`href="https://github.com/Layhak/khmer-holiday-api"`,
		`★ Star on GitHub`,
		`confirm that the recipient`,
		`lang="km"`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("homepage missing %q", want)
		}
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "img-src 'self'") {
		t.Errorf("Content-Security-Policy = %q, want self-hosted images allowed", got)
	}
	for _, directive := range []string{"connect-src 'self'", "script-src 'self'"} {
		if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, directive) {
			t.Errorf("Content-Security-Policy = %q, want %s", got, directive)
		}
	}
	if got := strings.Count(rec.Body.String(), `class="example"`); got != 5 {
		t.Errorf("example card count = %d, want 5", got)
	}
	if got := strings.Count(rec.Body.String(), `data-copy-target=`); got != 10 {
		t.Errorf("copy button count = %d, want 10", got)
	}
}

func TestDonationImageAndSearchFiles(t *testing.T) {
	s, _ := testServer(t)

	script := perform(s, "/assets/site.js")
	if script.Code != http.StatusOK {
		t.Fatalf("script status = %d, want 200", script.Code)
	}
	if got := script.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Errorf("script Content-Type = %q", got)
	}
	for _, want := range []string{"navigator.clipboard", "fetch(example.dataset.endpoint,"} {
		if !strings.Contains(script.Body.String(), want) {
			t.Errorf("site script missing %q", want)
		}
	}

	image := perform(s, "/support/aba-khqr.png")
	if image.Code != http.StatusOK {
		t.Fatalf("image status = %d, want 200", image.Code)
	}
	if got := image.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("image Content-Type = %q, want image/png", got)
	}
	if body := image.Body.Bytes(); len(body) < 8 || string(body[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatal("donation image is not a valid PNG")
	}

	preview := perform(s, "/assets/social-preview.png")
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", preview.Code)
	}
	if got := preview.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("preview Content-Type = %q, want image/png", got)
	}
	if body := preview.Body.Bytes(); len(body) < 8 || string(body[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatal("social preview is not a valid PNG")
	}
	config, err := png.DecodeConfig(bytes.NewReader(preview.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode social preview: %v", err)
	}
	if config.Width != 1200 || config.Height != 630 {
		t.Fatalf("social preview dimensions = %dx%d, want 1200x630",
			config.Width, config.Height)
	}

	robots := perform(s, "/robots.txt")
	if robots.Code != http.StatusOK || !strings.Contains(robots.Body.String(), "Sitemap: "+publicBaseURL+"/sitemap.xml") {
		t.Fatalf("unexpected robots.txt: status=%d body=%q", robots.Code, robots.Body.String())
	}

	sitemap := perform(s, "/sitemap.xml")
	if sitemap.Code != http.StatusOK || !strings.Contains(sitemap.Body.String(), "<loc>"+publicBaseURL+"/</loc>") {
		t.Fatalf("unexpected sitemap.xml: status=%d body=%q", sitemap.Code, sitemap.Body.String())
	}
}

func TestAPIRoutesAreExcludedFromSearchIndex(t *testing.T) {
	s, _ := testServer(t)
	rec := perform(s, "/api/v1/holidays?year=2027")

	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Errorf("X-Robots-Tag = %q, want noindex, nofollow", got)
	}
}

func TestInvalidFiltersAreRejectedAndNeverCached(t *testing.T) {
	s, _ := testServer(t)
	for _, target := range []string{
		"/api/v1/holidays?year=0",
		"/api/v1/holidays?month=0",
		"/api/v1/holidays?official=maybe",
		"/api/v1/holidays?key=UPPERCASE",
	} {
		rec := perform(s, target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", target, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want no-store", target, got)
		}
	}
}

func TestStatusSuppressesPrivateFailureDetails(t *testing.T) {
	s, st := testServer(t)
	err := st.RecordFetch(context.Background(), store.FetchRecord{
		Year:      2027,
		Source:    "mef",
		OK:        false,
		Note:      "helper failed with SECRET_TOKEN=do-not-publish",
		FetchedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := perform(s, "/api/v1/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "SECRET_TOKEN") {
		t.Fatal("public status leaked the private scraper failure detail")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestInternalErrorsAreGenericAndNeverCached(t *testing.T) {
	s, st := testServer(t)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	rec := perform(s, "/api/v1/years")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "internal server error" {
		t.Fatalf("public error = %q, want generic message", body["error"])
	}
}
