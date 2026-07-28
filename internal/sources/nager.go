package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
	"github.com/layhak/khmer-holiday-api/internal/model"
)

// Nager reads date.nager.at, an open holiday API with Cambodia coverage.
//
// It is the backbone of this project: free, no API key, stable JSON, and it
// carries both English and Khmer names. Crucially it also publishes future
// years - 2027 is already served today - by projecting the lunisolar calendar
// forward. Those projections are exactly why everything it returns is marked
// ConfidenceComputed and must be superseded once the sub-decree appears.
type Nager struct{ c *httpx.Client }

// NewNager constructs the adapter.
func NewNager(c *httpx.Client) *Nager { return &Nager{c: c} }

func (n *Nager) Name() string { return "nager" }

// Authority is computed: Nager tracks the sub-decree closely but is a
// third-party projection, not the legal document.
func (n *Nager) Authority() model.Confidence { return model.ConfidenceComputed }

type nagerHoliday struct {
	Date      string `json:"date"`
	LocalName string `json:"localName"`
	Name      string `json:"name"`
}

// Fetch pulls the year's holidays from the public v3 endpoint.
func (n *Nager) Fetch(ctx context.Context, year int) (*model.Snapshot, error) {
	url := fmt.Sprintf("https://date.nager.at/api/v3/PublicHolidays/%d/KH", year)

	body, err := n.c.Get(ctx, url)
	if err != nil {
		return nil, wrapStatus(err)
	}

	var raw []nagerHoliday
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("nager: decode %s: %w", url, err)
	}
	if len(raw) == 0 {
		return nil, ErrNotPublished
	}

	now := time.Now().UTC()
	out := make([]model.Holiday, 0, len(raw))
	for _, r := range raw {
		d, err := time.Parse(model.DateLayout, r.Date)
		if err != nil {
			return nil, fmt.Errorf("nager: bad date %q: %w", r.Date, err)
		}
		key := CanonKey(r.Name)
		out = append(out, model.Holiday{
			Date:      d,
			Key:       key,
			NameEN:    r.Name,
			NameKM:    r.LocalName,
			Conf:      model.ConfidenceComputed,
			Source:    n.Name(),
			SourceURL: url,
			UpdatedAt: now,
		})
	}

	return &model.Snapshot{
		Year:      year,
		Source:    n.Name(),
		SourceURL: url,
		Holidays:  Normalize(out),
		FetchedAt: now,
		Complete:  true,
	}, nil
}

// wrapStatus converts an HTTP status error into the package's sentinel errors
// so callers can distinguish "blocked" from "not published yet".
func wrapStatus(err error) error {
	var se *httpx.StatusError
	if ok := asStatusError(err, &se); ok {
		if se.Blocked() {
			return fmt.Errorf("%w: %v", ErrBlocked, se)
		}
		if se.Code == 404 {
			return fmt.Errorf("%w: %v", ErrNotPublished, se)
		}
	}
	return err
}
