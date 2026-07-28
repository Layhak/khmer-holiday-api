package sources

import (
	"testing"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func h(date time.Time, key, source string, conf model.Confidence) model.Holiday {
	return model.Holiday{
		Date: date, Key: key, NameEN: key, Conf: conf, Source: source,
		UpdatedAt: time.Now().UTC(),
	}
}

// A corroborating day count promotes computed dates to reported - but never
// straight to official, which requires reading the sub-decree itself.
func TestReconcilePromotesOnMatchingCount(t *testing.T) {
	dates := []model.Holiday{
		h(day(2026, time.January, 1), "intl_new_year", "nager", model.ConfidenceComputed),
		h(day(2026, time.January, 7), "victory_genocide", "nager", model.ConfidenceComputed),
	}

	got := Reconcile(2026, []*model.Snapshot{
		{Year: 2026, Source: "nager", Holidays: dates},
		{Year: 2026, Source: "akp", AnnouncedDays: 2, Decree: "Sub-Decree No. 167"},
	})

	if !got.CountMatches {
		t.Fatalf("CountMatches = false, want true (announced 2, held %d)", len(got.Holidays))
	}
	for _, hh := range got.Holidays {
		if hh.Conf != model.ConfidenceReported {
			t.Errorf("%s: confidence = %q, want %q", hh.Key, hh.Conf, model.ConfidenceReported)
		}
		if hh.Decree != "Sub-Decree No. 167" {
			t.Errorf("%s: decree = %q, want it stamped from the announcement", hh.Key, hh.Decree)
		}
	}
}

// A count mismatch must NOT promote, and must warn loudly - a silently wrong
// date is the failure mode that would corrupt a payroll calendar.
func TestReconcileWarnsOnCountMismatch(t *testing.T) {
	got := Reconcile(2026, []*model.Snapshot{
		{Year: 2026, Source: "nager", Holidays: []model.Holiday{
			h(day(2026, time.January, 1), "intl_new_year", "nager", model.ConfidenceComputed),
		}},
		{Year: 2026, Source: "akp", AnnouncedDays: 21},
	})

	if got.CountMatches {
		t.Fatal("CountMatches = true, want false")
	}
	if got.Holidays[0].Conf != model.ConfidenceComputed {
		t.Errorf("confidence = %q, want it left at %q on mismatch",
			got.Holidays[0].Conf, model.ConfidenceComputed)
	}
	if len(got.Warnings) == 0 {
		t.Error("want a warning describing the mismatch, got none")
	}
}

// Higher authority wins regardless of the order snapshots arrive in.
func TestReconcilePrefersHigherAuthority(t *testing.T) {
	d := day(2027, time.October, 1)

	got := Reconcile(2027, []*model.Snapshot{
		{Year: 2027, Source: "mlvt", Holidays: []model.Holiday{
			h(d, "pchum_ben", "mlvt", model.ConfidenceOfficial),
		}},
		{Year: 2027, Source: "nager", Holidays: []model.Holiday{
			h(d, "pchum_ben", "nager", model.ConfidenceComputed),
		}},
	})

	if len(got.Holidays) != 1 {
		t.Fatalf("got %d holidays, want 1 (same date must not duplicate)", len(got.Holidays))
	}
	if got.Holidays[0].Source != "mlvt" {
		t.Errorf("source = %q, want the official source to win", got.Holidays[0].Source)
	}
}

// Equal authority is broken by precedence: nager's dates beat wikipedia's.
func TestReconcilePrecedenceBreaksTies(t *testing.T) {
	d := day(2026, time.April, 14)

	for _, order := range [][]*model.Snapshot{
		{
			{Year: 2026, Source: "nager", Holidays: []model.Holiday{h(d, "khmer_new_year", "nager", model.ConfidenceComputed)}},
			{Year: 2026, Source: "wikipedia", Holidays: []model.Holiday{h(d, "khmer_new_year", "wikipedia", model.ConfidenceComputed)}},
		},
		{
			{Year: 2026, Source: "wikipedia", Holidays: []model.Holiday{h(d, "khmer_new_year", "wikipedia", model.ConfidenceComputed)}},
			{Year: 2026, Source: "nager", Holidays: []model.Holiday{h(d, "khmer_new_year", "nager", model.ConfidenceComputed)}},
		},
	} {
		got := Reconcile(2026, order)
		if got.Holidays[0].Source != "nager" {
			t.Errorf("source = %q, want nager to win on precedence regardless of order",
				got.Holidays[0].Source)
		}
	}
}

// Lunar dates left as projections must be flagged; this is the 2027 case.
func TestReconcileFlagsUnconfirmedLunarDates(t *testing.T) {
	got := Reconcile(2027, []*model.Snapshot{
		{Year: 2027, Source: "nager", Holidays: []model.Holiday{
			h(day(2027, time.September, 29), "pchum_ben", "nager", model.ConfidenceComputed),
		}},
	})

	if !got.Holidays[0].IsLunar {
		t.Error("pchum_ben should be flagged is_lunar")
	}
	if len(got.Warnings) == 0 {
		t.Error("want a warning about unconfirmed lunar dates, got none")
	}
}

func TestReconcileTracksCompleteSource(t *testing.T) {
	got := Reconcile(2027, []*model.Snapshot{
		{Year: 2027, Source: "wikipedia", Holidays: []model.Holiday{
			h(day(2027, time.January, 1), "intl_new_year", "wikipedia", model.ConfidenceComputed),
		}},
	})
	if got.Complete {
		t.Fatal("partial cross-check must not authorize destructive replacement")
	}

	got = Reconcile(2027, []*model.Snapshot{
		{Year: 2027, Source: "nager", Complete: true, Holidays: []model.Holiday{
			h(day(2027, time.January, 1), "intl_new_year", "nager", model.ConfidenceComputed),
		}},
	})
	if !got.Complete {
		t.Fatal("validated complete source should authorize replacement")
	}
}

func TestGroupMultiDaySplitsNonConsecutiveRuns(t *testing.T) {
	hs := Normalize([]model.Holiday{
		h(day(2026, time.April, 14), "khmer_new_year", "s", model.ConfidenceComputed),
		h(day(2026, time.April, 15), "khmer_new_year", "s", model.ConfidenceComputed),
		h(day(2026, time.April, 16), "khmer_new_year", "s", model.ConfidenceComputed),
		// A separate, non-adjacent day with the same key must form its own run.
		h(day(2026, time.December, 25), "khmer_new_year", "s", model.ConfidenceComputed),
	})

	want := []struct{ ord, of int }{{1, 3}, {2, 3}, {3, 3}, {1, 1}}
	for i, w := range want {
		if hs[i].Ordinal != w.ord || hs[i].OfDays != w.of {
			t.Errorf("day %s: got %d of %d, want %d of %d",
				hs[i].Date.Format(model.DateLayout), hs[i].Ordinal, hs[i].OfDays, w.ord, w.of)
		}
	}
}

func TestCanonKeyDisambiguatesSimilarNames(t *testing.T) {
	cases := map[string]string{
		"New Year's Day":                     "intl_new_year",
		"Khmer New Year":                     "khmer_new_year",
		"Cambodian New Year":                 "khmer_new_year",
		"King Sihamoni's Birthday":           "king_birthday",
		"Queen Mother's Birthday":            "queen_birthday",
		"Commemoration Day of King's Father": "kings_father",
		"Coronation Day of King Sihamoni":    "coronation_day",
		"Water Festival":                     "water_festival",
		"Bon Om Touk":                        "water_festival",
		"Pchum Ben":                          "pchum_ben",
		"Cambodia Peace Day":                 "peace_day",
	}
	for in, want := range cases {
		if got := CanonKey(in); got != want {
			t.Errorf("CanonKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKhmerNumeralRoundTrip(t *testing.T) {
	if got := toKhmerNumerals(2026); got != "២០២៦" {
		t.Errorf("toKhmerNumerals(2026) = %q, want ២០២៦", got)
	}
	if got := fromKhmerNumerals("២១៦"); got != "216" {
		t.Errorf("fromKhmerNumerals(២១៦) = %q, want 216", got)
	}
}

// A stray typo year in AKP's copy (observed: "September 05, 2925") must not
// hijack the binary search that walks the pagination.
func TestNewestDateIgnoresImplausibleYears(t *testing.T) {
	page := "AKP Phnom Penh, September 05, 2925 -- x " +
		"AKP Phnom Penh, September 05, 2025 -- y"

	got, ok := newestDate(page)
	if !ok {
		t.Fatal("newestDate returned no date")
	}
	if got.Year() != 2025 {
		t.Errorf("year = %d, want 2025 (the 2925 typo must be discarded)", got.Year())
	}
}
