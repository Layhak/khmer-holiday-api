package sources

import (
	"context"
	"fmt"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"
)

// VerifiedArchive contains past calendars transcribed and visually checked
// against the signed MLVT Prakas. Keeping these dates in source control makes
// historical official results reproducible instead of depending on a mutable
// third-party projection or an ignored SQLite file.
type VerifiedArchive struct{}

type verifiedYear struct {
	decree string
	url    string
	dates  []verifiedDate
}

type verifiedDate struct {
	date string
	key  string
}

var verifiedCalendars = map[int]verifiedYear{
	2025: {
		decree: "Prakas No. 218/24",
		url:    "https://mlvt.gov.kh/media/k2/attachments/20241018_218.pdf",
		dates: []verifiedDate{
			{"2025-01-01", "intl_new_year"},
			{"2025-01-07", "victory_genocide"},
			{"2025-03-08", "womens_day"},
			{"2025-04-14", "khmer_new_year"},
			{"2025-04-15", "khmer_new_year"},
			{"2025-04-16", "khmer_new_year"},
			{"2025-05-01", "labour_day"},
			{"2025-05-11", "visak_bochea"},
			{"2025-05-14", "king_birthday"},
			{"2025-05-15", "royal_ploughing"},
			{"2025-06-18", "queen_birthday"},
			{"2025-09-21", "pchum_ben"},
			{"2025-09-22", "pchum_ben"},
			{"2025-09-23", "pchum_ben"},
			{"2025-09-24", "constitution_day"},
			{"2025-10-15", "kings_father"},
			{"2025-10-29", "coronation_day"},
			{"2025-11-04", "water_festival"},
			{"2025-11-05", "water_festival"},
			{"2025-11-06", "water_festival"},
			{"2025-11-09", "independence_day"},
			{"2025-12-29", "peace_day"},
		},
	},
}

func NewVerifiedArchive() *VerifiedArchive { return &VerifiedArchive{} }

func (v *VerifiedArchive) Name() string { return "mlvt_verified" }

func (v *VerifiedArchive) Authority() model.Confidence { return model.ConfidenceOfficial }

func (v *VerifiedArchive) Fetch(_ context.Context, year int) (*model.Snapshot, error) {
	calendar, ok := verifiedCalendars[year]
	if !ok {
		return nil, fmt.Errorf("%w: verified archive has no calendar for %d",
			ErrNotPublished, year)
	}

	now := time.Now().UTC()
	holidays := make([]model.Holiday, 0, len(calendar.dates))
	for _, item := range calendar.dates {
		date, err := time.Parse(model.DateLayout, item.date)
		if err != nil {
			return nil, fmt.Errorf("verified archive: invalid date %q: %w", item.date, err)
		}
		nameEN, nameKM := CanonNames(item.key, item.key, "")
		holidays = append(holidays, model.Holiday{
			Date:      date,
			Key:       item.key,
			NameEN:    nameEN,
			NameKM:    nameKM,
			Conf:      model.ConfidenceOfficial,
			Source:    v.Name(),
			SourceURL: calendar.url,
			Decree:    calendar.decree,
			UpdatedAt: now,
		})
	}

	return &model.Snapshot{
		Year:        year,
		Source:      v.Name(),
		SourceURL:   calendar.url,
		DocumentURL: calendar.url,
		Decree:      calendar.decree,
		Holidays:    Normalize(holidays),
		FetchedAt:   now,
		Complete:    true,
		Note:        "Past calendar manually transcribed and visually verified against the signed MLVT Prakas.",
	}, nil
}
