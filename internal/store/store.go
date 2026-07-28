// Package store persists holidays in SQLite and answers filtered queries.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"

	_ "modernc.org/sqlite" // pure-Go driver, keeps the binary cgo-free
)

// Store is a handle to the holiday database.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS holidays (
    date        TEXT NOT NULL,           -- YYYY-MM-DD, the primary key
    year        INTEGER NOT NULL,
    month       INTEGER NOT NULL,
    day         INTEGER NOT NULL,
    key         TEXT NOT NULL,           -- stable slug, e.g. pchum_ben
    name_en     TEXT NOT NULL,
    name_km     TEXT NOT NULL DEFAULT '',
    ordinal     INTEGER NOT NULL DEFAULT 0,
    of_days     INTEGER NOT NULL DEFAULT 0,
    is_lunar    INTEGER NOT NULL DEFAULT 0,
    confidence  TEXT NOT NULL,           -- official | reported | computed
    source      TEXT NOT NULL,
    source_url  TEXT NOT NULL DEFAULT '',
    decree      TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (date)
);

CREATE INDEX IF NOT EXISTS idx_holidays_ymd  ON holidays(year, month, day);
CREATE INDEX IF NOT EXISTS idx_holidays_key  ON holidays(key);

-- One row per (year, source) recording the raw payload we ingested. This is
-- the audit trail: when a date changes, it shows which source changed it and
-- when, without needing the network again.
CREATE TABLE IF NOT EXISTS fetches (
    year        INTEGER NOT NULL,
    source      TEXT NOT NULL,
    source_url  TEXT NOT NULL DEFAULT '',
    decree      TEXT NOT NULL DEFAULT '',
    confidence  TEXT NOT NULL DEFAULT '',
    day_count   INTEGER NOT NULL DEFAULT 0,
    ok          INTEGER NOT NULL DEFAULT 0,
    note        TEXT NOT NULL DEFAULT '',
    fetched_at  TEXT NOT NULL,
    PRIMARY KEY (year, source)
);
`

// Open connects to the SQLite file at path and applies the schema.
func Open(ctx context.Context, path string) (*Store, error) {
	// _busy_timeout keeps concurrent scrape+serve from failing on a lock;
	// WAL lets the API read while a scrape writes.
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	// Building url.URL{Scheme: "file", Path: "data/holidays.db"} produces
	// file://data/holidays.db, where SQLite interprets "data" as a URI
	// authority. Prefixing the escaped path directly preserves both relative
	// and absolute filesystem semantics: file:data/... and file:/tmp/....
	escapedPath := (&url.URL{Path: path}).EscapedPath()
	dsn := "file:" + escapedPath + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Filter narrows a holiday query. Zero-valued fields are not applied, so an
// empty Filter returns every stored holiday.
type Filter struct {
	Year  int
	Month int
	Day   int

	// From and To bound the date range inclusively.
	From time.Time
	To   time.Time

	// Key selects a single holiday series, e.g. "khmer_new_year".
	Key string

	// OfficialOnly drops computed and reported dates.
	OfficialOnly bool
}

// Validate rejects filters that cannot match anything, so the API can return a
// 400 rather than a silently empty list.
func (f Filter) Validate() error {
	if f.Year < 0 || (f.Year > 0 && (f.Year < 1900 || f.Year > 2200)) {
		return fmt.Errorf("year must be between 1900 and 2200, got %d", f.Year)
	}
	if f.Month < 0 || f.Month > 12 {
		return fmt.Errorf("month must be 1-12, got %d", f.Month)
	}
	if f.Day < 0 || f.Day > 31 {
		return fmt.Errorf("day must be 1-31, got %d", f.Day)
	}
	if f.Day > 0 && f.Month == 0 && f.Year == 0 {
		// A bare ?day=14 across all months is legal but rarely intended;
		// allow it, it is a valid "14th of any month" query.
		return nil
	}
	if !f.From.IsZero() && !f.To.IsZero() && f.To.Before(f.From) {
		return fmt.Errorf("to (%s) is before from (%s)",
			f.To.Format(model.DateLayout), f.From.Format(model.DateLayout))
	}
	return nil
}

// List returns holidays matching f, ordered by date.
func (s *Store) List(ctx context.Context, f Filter) ([]model.Holiday, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}

	var (
		where []string
		args  []any
	)
	add := func(clause string, v any) {
		where = append(where, clause)
		args = append(args, v)
	}
	if f.Year > 0 {
		add("year = ?", f.Year)
	}
	if f.Month > 0 {
		add("month = ?", f.Month)
	}
	if f.Day > 0 {
		add("day = ?", f.Day)
	}
	if f.Key != "" {
		add("key = ?", f.Key)
	}
	if !f.From.IsZero() {
		add("date >= ?", f.From.Format(model.DateLayout))
	}
	if !f.To.IsZero() {
		add("date <= ?", f.To.Format(model.DateLayout))
	}
	if f.OfficialOnly {
		add("confidence = ?", string(model.ConfidenceOfficial))
	}

	q := `SELECT date, key, name_en, name_km, ordinal, of_days, is_lunar,
	             confidence, source, source_url, decree, updated_at
	      FROM holidays`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY date"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query holidays: %w", err)
	}
	defer rows.Close()

	out := []model.Holiday{}
	for rows.Next() {
		h, err := scanHoliday(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// KeyExists reports whether a canonical holiday key exists anywhere in the
// stored dataset. It lets the API distinguish an unknown key from a valid key
// that simply has no rows under the caller's other filters.
func (s *Store) KeyExists(ctx context.Context, key string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM holidays WHERE key = ? LIMIT 1)`, key,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check holiday key %q: %w", key, err)
	}
	return exists, nil
}

// Upsert writes holidays, letting better-sourced data win.
//
// The reconciliation rule: a row is replaced only when the incoming record has
// strictly higher confidence, or equal confidence and a newer timestamp. This
// is what stops a computed 2027 projection from overwriting the official
// sub-decree date once we have it, regardless of scrape order.
func (s *Store) Upsert(ctx context.Context, hs []model.Holiday) (inserted, updated, skipped int, err error) {
	if err := validateHolidays(hs, 0); err != nil {
		return 0, 0, 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	inserted, updated, skipped, err = upsertTx(ctx, tx, hs)
	if err != nil {
		return 0, 0, 0, err
	}
	return inserted, updated, skipped, tx.Commit()
}

func upsertTx(ctx context.Context, tx *sql.Tx, hs []model.Holiday) (inserted, updated, skipped int, err error) {
	sel, err := tx.PrepareContext(ctx, `SELECT confidence, updated_at FROM holidays WHERE date = ?`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer sel.Close()

	ins, err := tx.PrepareContext(ctx, `
		INSERT INTO holidays (date, year, month, day, key, name_en, name_km,
		                      ordinal, of_days, is_lunar, confidence, source,
		                      source_url, decree, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(date) DO UPDATE SET
			key=excluded.key, name_en=excluded.name_en, name_km=excluded.name_km,
			ordinal=excluded.ordinal, of_days=excluded.of_days,
			is_lunar=excluded.is_lunar, confidence=excluded.confidence,
			source=excluded.source, source_url=excluded.source_url,
			decree=excluded.decree, updated_at=excluded.updated_at`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer ins.Close()

	for _, h := range hs {
		var curConf, curUpdated string
		row := sel.QueryRowContext(ctx, h.Date.Format(model.DateLayout))
		switch scanErr := row.Scan(&curConf, &curUpdated); {
		case scanErr == sql.ErrNoRows:
			// new date, fall through to insert
		case scanErr != nil:
			return 0, 0, 0, fmt.Errorf("lookup %s: %w", h.Date.Format(model.DateLayout), scanErr)
		default:
			existing := model.Confidence(curConf)
			if h.Conf.Rank() < existing.Rank() {
				skipped++
				continue
			}
			if h.Conf.Rank() == existing.Rank() {
				if prev, perr := time.Parse(time.RFC3339, curUpdated); perr == nil &&
					!h.UpdatedAt.After(prev) {
					skipped++
					continue
				}
			}
			updated++
		}

		if h.UpdatedAt.IsZero() {
			h.UpdatedAt = time.Now().UTC()
		}
		lunar := 0
		if h.IsLunar {
			lunar = 1
		}
		if _, err := ins.ExecContext(ctx,
			h.Date.Format(model.DateLayout), h.Date.Year(), int(h.Date.Month()), h.Date.Day(),
			h.Key, h.NameEN, h.NameKM, h.Ordinal, h.OfDays, lunar,
			string(h.Conf), h.Source, h.SourceURL, h.Decree,
			h.UpdatedAt.UTC().Format(time.RFC3339),
		); err != nil {
			return 0, 0, 0, fmt.Errorf("upsert %s: %w", h.Date.Format(model.DateLayout), err)
		}
	}

	inserted = len(hs) - updated - skipped
	return inserted, updated, skipped, nil
}

// ReplaceYear atomically swaps a year's rows. Validation and inserts happen in
// the same transaction as deletion, so a malformed scrape or database error
// cannot erase the previously served dataset.
func (s *Store) ReplaceYear(ctx context.Context, year int, hs []model.Holiday) (removed, inserted int, err error) {
	if year < 1900 || year > 2200 {
		return 0, 0, fmt.Errorf("implausible year %d", year)
	}
	if len(hs) == 0 {
		return 0, 0, fmt.Errorf("refusing to replace year %d with an empty dataset", year)
	}
	if err := validateHolidays(hs, year); err != nil {
		return 0, 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin replacement: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `DELETE FROM holidays WHERE year = ?`, year)
	if err != nil {
		return 0, 0, fmt.Errorf("delete year %d: %w", year, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, 0, fmt.Errorf("count deleted rows for %d: %w", year, err)
	}

	inserted, _, _, err = upsertTx(ctx, tx, hs)
	if err != nil {
		return 0, 0, fmt.Errorf("replace year %d: %w", year, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit replacement for %d: %w", year, err)
	}
	return int(n), inserted, nil
}

func validateHolidays(hs []model.Holiday, expectedYear int) error {
	seen := make(map[string]struct{}, len(hs))
	for _, h := range hs {
		if err := h.Validate(); err != nil {
			return err
		}
		if expectedYear != 0 && h.Date.Year() != expectedYear {
			return fmt.Errorf("holiday %s belongs to %d, not replacement year %d",
				h.Date.Format(model.DateLayout), h.Date.Year(), expectedYear)
		}
		date := h.Date.Format(model.DateLayout)
		if _, ok := seen[date]; ok {
			return fmt.Errorf("duplicate holiday date %s", date)
		}
		seen[date] = struct{}{}
	}
	return nil
}

// Years lists the years that have data, ascending.
func (s *Store) Years(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT year FROM holidays ORDER BY year`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []int{}
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, err
		}
		out = append(out, y)
	}
	return out, rows.Err()
}

func scanHoliday(rows *sql.Rows) (model.Holiday, error) {
	var (
		h             model.Holiday
		date, updated string
		conf          string
		lunar         int
	)
	if err := rows.Scan(&date, &h.Key, &h.NameEN, &h.NameKM, &h.Ordinal, &h.OfDays,
		&lunar, &conf, &h.Source, &h.SourceURL, &h.Decree, &updated); err != nil {
		return h, fmt.Errorf("scan holiday: %w", err)
	}
	d, err := time.Parse(model.DateLayout, date)
	if err != nil {
		return h, fmt.Errorf("parse date %q: %w", date, err)
	}
	h.Date = d
	h.Conf = model.Confidence(conf)
	h.IsLunar = lunar == 1
	if u, err := time.Parse(time.RFC3339, updated); err == nil {
		h.UpdatedAt = u
	}
	return h, nil
}
