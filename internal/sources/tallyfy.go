package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
	"github.com/layhak/khmer-holiday-api/internal/model"
)

const tallyfyEndpoint = "https://tallyfy.com/national-holidays/api/KH/%d.json"

// Tallyfy reads Tallyfy's public Cambodia holiday JSON feed. Tallyfy is a
// third-party calendar rather than a legal or government source, and it
// publishes several future years. Its rows therefore remain computed
// projections and are intentionally lower precedence than Wikipedia and Nager.
type Tallyfy struct {
	c        *httpx.Client
	endpoint string
}

// NewTallyfy constructs the adapter.
func NewTallyfy(c *httpx.Client) *Tallyfy {
	return &Tallyfy{c: c, endpoint: tallyfyEndpoint}
}

func (t *Tallyfy) Name() string { return "tallyfy" }

func (t *Tallyfy) Authority() model.Confidence { return model.ConfidenceComputed }

type tallyfyPayload struct {
	Country struct {
		Code string `json:"code"`
	} `json:"country"`
	Year     int `json:"year"`
	Holidays []struct {
		Date              string `json:"date"`
		Name              string `json:"name"`
		LocalName         string `json:"local_name"`
		Type              string `json:"type"`
		ObservedDate      string `json:"observed_date"`
		IsObservedShifted bool   `json:"is_observed_shifted"`
	} `json:"holidays"`
}

// Fetch returns national holidays only. Bank-only closure dates are excluded
// because this API models public holidays that apply generally.
func (t *Tallyfy) Fetch(ctx context.Context, year int) (*model.Snapshot, error) {
	url := fmt.Sprintf(t.endpoint, year)
	body, err := t.c.Get(ctx, url)
	if err != nil {
		return nil, wrapStatus(err)
	}

	var payload tallyfyPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("tallyfy: decode %s: %w", url, err)
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Country.Code), "KH") {
		return nil, fmt.Errorf("tallyfy: response country %q is not KH", payload.Country.Code)
	}
	if payload.Year != year {
		return nil, fmt.Errorf("tallyfy: response year %d does not match requested year %d",
			payload.Year, year)
	}

	now := time.Now().UTC()
	byDate := make(map[string]model.Holiday, len(payload.Holidays))
	collidedDates := map[string]struct{}{}
	for i, raw := range payload.Holidays {
		if !strings.EqualFold(strings.TrimSpace(raw.Type), "national") {
			continue
		}

		dateRaw := strings.TrimSpace(raw.Date)
		if observed := strings.TrimSpace(raw.ObservedDate); observed != "" {
			dateRaw = observed
		}
		date, err := time.Parse(model.DateLayout, dateRaw)
		if err != nil {
			return nil, fmt.Errorf("tallyfy: holiday %d has bad date %q: %w",
				i+1, dateRaw, err)
		}

		name := strings.TrimSpace(raw.Name)
		localName := strings.TrimSpace(raw.LocalName)
		holiday := model.Holiday{
			Date:      date,
			Key:       CanonKey(name),
			NameEN:    name,
			NameKM:    localName,
			Conf:      model.ConfidenceComputed,
			Source:    t.Name(),
			SourceURL: url,
			UpdatedAt: now,
		}

		dateKey := date.Format(model.DateLayout)
		if _, collided := collidedDates[dateKey]; collided {
			continue
		}
		if existing, duplicate := byDate[dateKey]; duplicate {
			if existing.Key == holiday.Key {
				continue
			}
			// The core model intentionally stores one row per date. When
			// Tallyfy assigns two distinct holidays to the same day, omit the
			// ambiguous date from this low-confidence cross-check. A stronger
			// source can still supply the actual day and holiday label.
			delete(byDate, dateKey)
			collidedDates[dateKey] = struct{}{}
			continue
		}
		byDate[dateKey] = holiday
	}

	out := make([]model.Holiday, 0, len(byDate))
	for _, holiday := range byDate {
		out = append(out, holiday)
	}
	if len(out) == 0 {
		return nil, ErrNotPublished
	}

	note := "third-party future-year projection; does not authorize replacement"
	if len(collidedDates) > 0 {
		note += fmt.Sprintf("; excluded %d date(s) carrying multiple holidays", len(collidedDates))
	}

	return &model.Snapshot{
		Year:      year,
		Source:    t.Name(),
		SourceURL: url,
		Holidays:  Normalize(out),
		FetchedAt: now,
		// This projection is useful as a cross-check but must never authorize
		// destructive replacement of a year.
		Complete: false,
		Note:     note,
	}, nil
}
