package sources

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
	"github.com/layhak/khmer-holiday-api/internal/model"
)

// NBC reads the National Bank of Cambodia's official public-holiday calendar.
//
// Unlike the scanned MLVT Prakas, NBC publishes the current year's dates as a
// small HTML table. That makes it the strongest machine-readable source
// currently available to the scraper.
type NBC struct {
	c        *httpx.Client
	endpoint string
}

const nbcHolidayURL = "https://www.nbc.gov.kh/english/news_and_events/official_holiday.php"

var (
	nbcYearRe  = regexp.MustCompile(`(?i)Public\s+Holidays\s+(\d{4})`)
	nbcTableRe = regexp.MustCompile(`(?is)<table[^>]*class=["'][^"']*\bgeneral-2\b[^"']*["'][^>]*>(.*?)</table>`)
	nbcRowRe   = regexp.MustCompile(`(?is)<tr>\s*<td[^>]*>(.*?)</td>\s*<td[^>]*>(.*?)</td>\s*</tr>`)
	nbcDateRe  = regexp.MustCompile(`^(\d{1,2}(?:-\d{1,2})*)\s+([A-Za-z]{3})$`)
)

// NewNBC constructs the adapter.
func NewNBC(c *httpx.Client) *NBC {
	return &NBC{c: c, endpoint: nbcHolidayURL}
}

func (n *NBC) Name() string { return "nbc" }

// Authority is official because NBC is a Cambodian government institution
// publishing its own annual closure calendar.
func (n *NBC) Authority() model.Confidence { return model.ConfidenceOfficial }

// Fetch parses the complete holiday table for the year currently published by
// NBC. The page exposes one year at a time, so other years are NotPublished.
func (n *NBC) Fetch(ctx context.Context, year int) (*model.Snapshot, error) {
	body, err := n.c.Get(ctx, n.endpoint)
	if err != nil {
		return nil, wrapStatus(err)
	}
	page := string(body)

	yearMatch := nbcYearRe.FindStringSubmatch(stripHTML(page))
	if yearMatch == nil {
		return nil, fmt.Errorf("nbc: public-holiday page has no identifiable year")
	}
	publishedYear, err := strconv.Atoi(yearMatch[1])
	if err != nil {
		return nil, fmt.Errorf("nbc: invalid published year %q: %w", yearMatch[1], err)
	}
	if publishedYear != year {
		return nil, fmt.Errorf("%w: nbc currently publishes %d, not %d",
			ErrNotPublished, publishedYear, year)
	}

	tableMatch := nbcTableRe.FindStringSubmatch(page)
	if tableMatch == nil {
		return nil, fmt.Errorf("nbc: public-holiday table is missing")
	}

	now := time.Now().UTC()
	holidays := make([]model.Holiday, 0, 24)
	for _, row := range nbcRowRe.FindAllStringSubmatch(tableMatch[1], -1) {
		dateLabel := html2text(stripHTML(row[1]))
		name := html2text(stripHTML(row[2]))
		dateMatch := nbcDateRe.FindStringSubmatch(dateLabel)
		if dateMatch == nil || name == "" {
			continue
		}

		for _, rawDay := range strings.Split(dateMatch[1], "-") {
			day, err := strconv.Atoi(rawDay)
			if err != nil {
				return nil, fmt.Errorf("nbc: invalid day %q in %q: %w", rawDay, dateLabel, err)
			}
			date, err := time.Parse("2 Jan 2006",
				fmt.Sprintf("%d %s %d", day, dateMatch[2], year))
			if err != nil {
				return nil, fmt.Errorf("nbc: invalid date %q: %w", dateLabel, err)
			}

			holidays = append(holidays, model.Holiday{
				Date:      date,
				Key:       CanonKey(name),
				NameEN:    name,
				Conf:      model.ConfidenceOfficial,
				Source:    n.Name(),
				SourceURL: n.endpoint,
				UpdatedAt: now,
			})
		}
	}

	// A complete Cambodian calendar has historically contained well over ten
	// days. Reject a partially rendered or structurally changed page rather
	// than allowing an official source to authorize destructive replacement.
	if len(holidays) < 10 || len(holidays) > 40 {
		return nil, fmt.Errorf("nbc: parsed implausible holiday count %d", len(holidays))
	}

	return &model.Snapshot{
		Year:      year,
		Source:    n.Name(),
		SourceURL: n.endpoint,
		Holidays:  Normalize(holidays),
		FetchedAt: now,
		Complete:  true,
		Note:      "Official National Bank of Cambodia public-holiday calendar.",
	}, nil
}
