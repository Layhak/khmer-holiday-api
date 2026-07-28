package store

import (
	"context"
	"fmt"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"
)

// FetchRecord is the audit row for one source's attempt at one year.
type FetchRecord struct {
	Year       int              `json:"year"`
	Source     string           `json:"source"`
	SourceURL  string           `json:"source_url,omitempty"`
	Decree     string           `json:"decree,omitempty"`
	Confidence model.Confidence `json:"confidence,omitempty"`
	DayCount   int              `json:"day_count"`
	OK         bool             `json:"ok"`
	Note       string           `json:"note,omitempty"`
	FetchedAt  time.Time        `json:"fetched_at"`
}

// RecordFetch stores the outcome of a scrape attempt, success or failure.
// Failures are recorded deliberately: knowing that mef.gov.kh has been
// returning 403 for six months is operationally useful.
func (s *Store) RecordFetch(ctx context.Context, r FetchRecord) error {
	ok := 0
	if r.OK {
		ok = 1
	}
	if r.FetchedAt.IsZero() {
		r.FetchedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fetches (year, source, source_url, decree, confidence,
		                     day_count, ok, note, fetched_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(year, source) DO UPDATE SET
			source_url=excluded.source_url, decree=excluded.decree,
			confidence=excluded.confidence, day_count=excluded.day_count,
			ok=excluded.ok, note=excluded.note, fetched_at=excluded.fetched_at`,
		r.Year, r.Source, r.SourceURL, r.Decree, string(r.Confidence),
		r.DayCount, ok, r.Note, r.FetchedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record fetch %s/%d: %w", r.Source, r.Year, err)
	}
	return nil
}

// Fetches returns the audit trail, newest year first. A year of 0 returns all.
func (s *Store) Fetches(ctx context.Context, year int) ([]FetchRecord, error) {
	q := `SELECT year, source, source_url, decree, confidence, day_count, ok,
	             note, fetched_at FROM fetches`
	args := []any{}
	if year > 0 {
		q += " WHERE year = ?"
		args = append(args, year)
	}
	q += " ORDER BY year DESC, source"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query fetches: %w", err)
	}
	defer rows.Close()

	out := []FetchRecord{}
	for rows.Next() {
		var (
			r       FetchRecord
			conf    string
			ok      int
			fetched string
		)
		if err := rows.Scan(&r.Year, &r.Source, &r.SourceURL, &r.Decree, &conf,
			&r.DayCount, &ok, &r.Note, &fetched); err != nil {
			return nil, fmt.Errorf("scan fetch: %w", err)
		}
		r.Confidence = model.Confidence(conf)
		r.OK = ok == 1
		if t, err := time.Parse(time.RFC3339, fetched); err == nil {
			r.FetchedAt = t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// YearStatus summarises coverage for one year.
type YearStatus struct {
	Year        int    `json:"year"`
	Days        int    `json:"days"`
	Official    int    `json:"official"`
	Reported    int    `json:"reported"`
	Computed    int    `json:"computed"`
	Decree      string `json:"decree,omitempty"`
	Provisional bool   `json:"provisional"`
}

// Status reports per-year coverage and whether any date is still unconfirmed.
// This is what tells you at a glance that 2027 is not yet nailed down.
func (s *Store) Status(ctx context.Context) ([]YearStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT year,
		       COUNT(*),
		       SUM(CASE WHEN confidence='official' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN confidence='reported' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN confidence='computed' THEN 1 ELSE 0 END),
		       COALESCE(MAX(decree),'')
		FROM holidays GROUP BY year ORDER BY year`)
	if err != nil {
		return nil, fmt.Errorf("query status: %w", err)
	}
	defer rows.Close()

	out := []YearStatus{}
	for rows.Next() {
		var st YearStatus
		if err := rows.Scan(&st.Year, &st.Days, &st.Official, &st.Reported,
			&st.Computed, &st.Decree); err != nil {
			return nil, fmt.Errorf("scan status: %w", err)
		}
		st.Provisional = st.Official < st.Days
		out = append(out, st)
	}
	return out, rows.Err()
}
