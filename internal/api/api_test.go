package api

import (
	"context"
	"encoding/json"
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
