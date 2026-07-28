package sources

import (
	"fmt"
	"sort"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"
)

// Reconciliation is the merged outcome for one year across all sources.
type Reconciliation struct {
	Year     int
	Holidays []model.Holiday

	// Decree and DocumentURL come from the most authoritative source that
	// identified the governing legal instrument.
	Decree      string
	DocumentURL string

	// AnnouncedDays is the official total, when a source stated one.
	AnnouncedDays int

	// CountMatches is true when AnnouncedDays equals the number of days we
	// actually hold. This is the check that lets computed dates be promoted.
	CountMatches bool

	// Warnings describe disagreements an operator should look at.
	Warnings []string
}

// Reconcile merges per-source snapshots into a single set of holidays.
//
// The rules, in order:
//
//  1. Dates come from snapshots that actually carry dates. Where two sources
//     disagree on a date for the same holiday key, the higher-authority source
//     wins; ties go to the source that ran later.
//
//  2. Evidence-only snapshots (AKP's day count, MLVT's Prakas link) never
//     contribute dates, but they supply the decree reference and the expected
//     day count.
//
//  3. If an evidence source announced N days and we hold exactly N days, the
//     dataset is corroborated and every computed row is promoted to "reported".
//     It is NOT promoted to "official" - that requires reading the sub-decree
//     itself, which is a scanned PDF. Use `khapi verify` for that final step.
//
//  4. If the counts disagree, nothing is promoted and a warning is recorded.
//     A silent mismatch is the failure mode that would put a wrong date in a
//     payroll system, so it is surfaced loudly instead.
func Reconcile(year int, snaps []*model.Snapshot) *Reconciliation {
	r := &Reconciliation{Year: year}

	// Strongest authority last, so later writes win on equal keys.
	ordered := make([]*model.Snapshot, 0, len(snaps))
	for _, s := range snaps {
		if s != nil {
			ordered = append(ordered, s)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if authorityOf(a).Rank() != authorityOf(b).Rank() {
			return authorityOf(a).Rank() < authorityOf(b).Rank()
		}
		return Precedence(a.Source) < Precedence(b.Source)
	})

	byDate := map[string]model.Holiday{}

	for _, s := range ordered {
		if s.Decree != "" {
			r.Decree = s.Decree
		}
		if s.DocumentURL != "" {
			r.DocumentURL = s.DocumentURL
		}
		if s.AnnouncedDays > 0 {
			r.AnnouncedDays = s.AnnouncedDays
		}

		for _, h := range s.Holidays {
			k := h.Date.Format(model.DateLayout)
			prev, exists := byDate[k]
			if exists {
				if h.Conf.Rank() < prev.Conf.Rank() {
					continue
				}
				// Equal authority: keep the higher-precedence source's row.
				if h.Conf.Rank() == prev.Conf.Rank() &&
					Precedence(h.Source) < Precedence(prev.Source) {
					continue
				}
			}
			byDate[k] = h
		}
	}

	for _, h := range byDate {
		r.Holidays = append(r.Holidays, h)
	}

	// Normalize here, not at the end: it is what sets IsLunar, and the checks
	// below depend on that flag. Adapters normalize their own output too, but
	// the reconciler must not rely on them having done so.
	r.Holidays = Normalize(r.Holidays)

	// Rule 3/4: corroborate the count.
	if r.AnnouncedDays > 0 && len(r.Holidays) > 0 {
		r.CountMatches = r.AnnouncedDays == len(r.Holidays)
		if r.CountMatches {
			for i := range r.Holidays {
				if r.Holidays[i].Conf == model.ConfidenceComputed {
					r.Holidays[i].Conf = model.ConfidenceReported
				}
			}
		} else {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"day-count mismatch for %d: official announcement says %d days, "+
					"but we hold %d - dates left unpromoted, verify against %s",
				year, r.AnnouncedDays, len(r.Holidays), fallback(r.DocumentURL, "the sub-decree")))
		}
	}

	// Stamp the decree reference onto every row we kept.
	if r.Decree != "" {
		for i := range r.Holidays {
			if r.Holidays[i].Decree == "" {
				r.Holidays[i].Decree = r.Decree
			}
		}
	}

	// Warn about lunar dates that remain merely computed - these are the ones
	// that actually move between a projection and the signed sub-decree.
	unconfirmed := 0
	for _, h := range r.Holidays {
		if h.IsLunar && h.Conf == model.ConfidenceComputed {
			unconfirmed++
		}
	}
	if unconfirmed > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"%d lunar holiday day(s) in %d are still computed projections and may shift "+
				"when the sub-decree is published", unconfirmed, year))
	}

	return r
}

// authorityOf reports the confidence a snapshot's rows carry, falling back to
// the snapshot's strongest row when it has no explicit authority.
func authorityOf(s *model.Snapshot) model.Confidence {
	best := model.ConfidenceComputed
	for _, h := range s.Holidays {
		if h.Conf.Rank() > best.Rank() {
			best = h.Conf
		}
	}
	return best
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Promote raises every holiday in hs to the given confidence and stamps the
// decree reference. It backs `khapi verify`, the human-in-the-loop step that
// marks a year as confirmed against the signed document.
func Promote(hs []model.Holiday, conf model.Confidence, decree, docURL string) []model.Holiday {
	now := time.Now().UTC()
	out := make([]model.Holiday, len(hs))
	copy(out, hs)
	for i := range out {
		out[i].Conf = conf
		if decree != "" {
			out[i].Decree = decree
		}
		if docURL != "" {
			out[i].SourceURL = docURL
		}
		out[i].UpdatedAt = now
	}
	return out
}
