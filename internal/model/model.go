// Package model defines the core types shared by the store, sources and API.
package model

import (
	"fmt"
	"strings"
	"time"
)

// DateLayout is the canonical wire and storage format for a holiday date.
const DateLayout = "2006-01-02"

// Confidence describes how much trust a holiday date deserves.
//
// Cambodian public holidays are fixed each year by a sub-decree (អនុក្រឹត្យ)
// signed by the Prime Minister, normally around September of the preceding
// year. Until that sub-decree exists, the lunar holidays (Pchum Ben, Water
// Festival, Royal Ploughing Ceremony, Meak Bochea, Visak Bochea) can only be
// predicted from the Khmer lunisolar calendar, and predictions do shift.
//
// Callers that care about correctness - payroll, banking, SLA calendars -
// should treat anything below ConfidenceOfficial as provisional.
type Confidence string

const (
	// ConfidenceOfficial means the date was taken from the sub-decree itself
	// or from a government publication reproducing it.
	ConfidenceOfficial Confidence = "official"

	// ConfidenceReported means a reputable secondary source (state news
	// agency, bank, chamber of commerce) reported the date, but we have not
	// matched it to the sub-decree document.
	ConfidenceReported Confidence = "reported"

	// ConfidenceComputed means the date was derived from the lunisolar
	// calendar or a third-party API's own projection. Subject to change.
	ConfidenceComputed Confidence = "computed"
)

// Rank orders confidence levels so the reconciler can prefer better data.
func (c Confidence) Rank() int {
	switch c {
	case ConfidenceOfficial:
		return 3
	case ConfidenceReported:
		return 2
	case ConfidenceComputed:
		return 1
	default:
		return 0
	}
}

// Lunar reports whether a holiday's date is set by the Khmer lunisolar
// calendar and therefore moves from year to year. Fixed-date holidays such as
// New Year's Day are safe to project forward; these are not.
func Lunar(key string) bool {
	switch key {
	case "pchum_ben", "water_festival", "royal_ploughing", "meak_bochea", "visak_bochea":
		return true
	}
	return false
}

// Holiday is a single public holiday day.
//
// A multi-day festival such as Pchum Ben is stored as one row per day, each
// carrying the same Key, so that a date lookup is a plain equality match.
// Ordinal/OfDays describe the day's position within its festival.
type Holiday struct {
	Date      time.Time  `json:"-"`
	Key       string     `json:"key"`
	NameEN    string     `json:"name_en"`
	NameKM    string     `json:"name_km,omitempty"`
	Ordinal   int        `json:"ordinal,omitempty"`
	OfDays    int        `json:"of_days,omitempty"`
	IsLunar   bool       `json:"is_lunar"`
	Conf      Confidence `json:"confidence"`
	Source    string     `json:"source"`
	SourceURL string     `json:"source_url,omitempty"`
	Decree    string     `json:"decree,omitempty"`
	UpdatedAt time.Time  `json:"-"`
}

// MarshalJSON renders the date fields the API promises alongside the struct.
func (h Holiday) MarshalJSON() ([]byte, error) {
	type alias Holiday // avoid recursing into this method
	return marshalWithDates(alias(h), h.Date, h.UpdatedAt)
}

// Year, Month and Day expose the filter dimensions the API supports.
func (h Holiday) Year() int  { return h.Date.Year() }
func (h Holiday) Month() int { return int(h.Date.Month()) }
func (h Holiday) Day() int   { return h.Date.Day() }

// Official reports whether the date is confirmed by a sub-decree.
func (h Holiday) Official() bool { return h.Conf == ConfidenceOfficial }

// Validate catches malformed records before they reach the database.
func (h Holiday) Validate() error {
	if h.Date.IsZero() {
		return fmt.Errorf("holiday %q: date is zero", h.Key)
	}
	if strings.TrimSpace(h.Key) == "" {
		return fmt.Errorf("holiday on %s: key is empty", h.Date.Format(DateLayout))
	}
	if strings.TrimSpace(h.NameEN) == "" {
		return fmt.Errorf("holiday %q: name_en is empty", h.Key)
	}
	if h.Conf.Rank() == 0 {
		return fmt.Errorf("holiday %q: unknown confidence %q", h.Key, h.Conf)
	}
	if strings.TrimSpace(h.Source) == "" {
		return fmt.Errorf("holiday %q: source is empty", h.Key)
	}
	return nil
}

// Snapshot is one source's complete answer for one year. The reconciler merges
// snapshots rather than individual holidays, because "this source lists 21 days
// for 2026" is a stronger signal than any single row.
type Snapshot struct {
	Year      int
	Source    string
	SourceURL string
	Decree    string
	Holidays  []Holiday
	FetchedAt time.Time

	// AnnouncedDays is the total number of holiday days the source states for
	// the year, when it reports a total without listing every date. The state
	// news agency does this: "21 days of public holidays for 2026". Comparing
	// it against the day count we actually hold is a cheap, strong check that
	// our dates are complete.
	AnnouncedDays int

	// DocumentURL points at the primary legal document (the sub-decree or
	// Prakas PDF) when the source publishes one.
	DocumentURL string

	// Note carries a human-readable remark for the audit trail, such as why a
	// source returned no dates.
	Note string
}

// EvidenceOnly reports whether the snapshot carries provenance but no dates.
// The Ministry of Labour publishes the holiday Prakas as a scanned PDF with no
// text layer, so it can prove which decree governs a year without yielding
// machine-readable dates.
func (s Snapshot) EvidenceOnly() bool {
	return len(s.Holidays) == 0 && (s.Decree != "" || s.DocumentURL != "" || s.AnnouncedDays > 0)
}
