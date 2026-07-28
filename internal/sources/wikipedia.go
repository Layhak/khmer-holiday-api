package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
	"github.com/layhak/khmer-holiday-api/internal/model"
)

// Wikipedia reads the "Public holidays in Cambodia" article through the
// MediaWiki API.
//
// It is used through the API rather than by scraping HTML because the wikitext
// endpoint is a stable, documented contract that will not break on a skin
// change. Its value here is the fixed-date holidays and the Khmer-script names;
// it deliberately does NOT emit lunar holidays, because the article lists those
// as "Moveable, April or May" rather than as a date. Trying to guess a date
// from that would manufacture false precision.
type Wikipedia struct{ c *httpx.Client }

// NewWikipedia constructs the adapter.
func NewWikipedia(c *httpx.Client) *Wikipedia { return &Wikipedia{c: c} }

func (w *Wikipedia) Name() string { return "wikipedia" }

// Authority is computed: an encyclopedia summary, not a legal instrument.
func (w *Wikipedia) Authority() model.Confidence { return model.ConfidenceComputed }

const wikiAPI = "https://en.wikipedia.org/w/api.php?action=parse" +
	"&page=Public_holidays_in_Cambodia&prop=wikitext&format=json&formatversion=2"

var (
	// Row separator in a wikitable.
	wikiRowRe = regexp.MustCompile(`(?m)^\|-`)
	// {{lang|km|...}} carries the Khmer name.
	wikiLangRe = regexp.MustCompile(`\{\{lang\|km\|([^}]+)\}\}`)
	// "January 1" or "April 14-16" / "April 14–16".
	wikiDateRe = regexp.MustCompile(`(?i)\b(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{1,2})(?:\s*[-–—]\s*(\d{1,2}))?`)
)

// Fetch returns the fixed-date holidays for the year.
func (w *Wikipedia) Fetch(ctx context.Context, year int) (*model.Snapshot, error) {
	body, err := w.c.Get(ctx, wikiAPI)
	if err != nil {
		return nil, wrapStatus(err)
	}

	var payload struct {
		Parse struct {
			Wikitext string `json:"wikitext"`
		} `json:"parse"`
		Error *struct {
			Info string `json:"info"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("wikipedia: decode response: %w", err)
	}
	if payload.Error != nil {
		return nil, fmt.Errorf("wikipedia: api error: %s", payload.Error.Info)
	}
	text := payload.Parse.Wikitext
	if strings.TrimSpace(text) == "" {
		return nil, ErrNotPublished
	}

	now := time.Now().UTC()
	seen := map[string]bool{}
	out := []model.Holiday{}

	for _, row := range wikiRowRe.Split(text, -1) {
		// A row begins with "\n|", so splitting on that separator yields an
		// empty leading element; drop empties to get the real cells.
		cells := []string{}
		for _, c := range strings.Split(row, "\n|") {
			if strings.TrimSpace(c) != "" {
				cells = append(cells, c)
			}
		}
		if len(cells) < 2 {
			continue
		}
		nameCell, dateCell := cells[0], cells[1]

		km := ""
		if m := wikiLangRe.FindStringSubmatch(nameCell); m != nil {
			km = strings.TrimSpace(m[1])
		}

		nameEN := cleanWikitext(wikiLangRe.ReplaceAllString(nameCell, ""))
		if nameEN == "" {
			continue
		}
		key := CanonKey(nameEN)

		// Lunar holidays have no usable date in this article.
		if model.Lunar(key) {
			continue
		}

		m := wikiDateRe.FindStringSubmatch(dateCell)
		if m == nil {
			continue
		}
		month, ok := monthFromName(m[1])
		if !ok {
			continue
		}
		start, _ := strconv.Atoi(m[2])
		end := start
		if m[3] != "" {
			if e, err := strconv.Atoi(m[3]); err == nil && e >= start {
				end = e
			}
		}

		for d := start; d <= end; d++ {
			date := time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
			// Guard against a malformed day rolling into the next month.
			if date.Month() != month {
				continue
			}
			ds := date.Format(model.DateLayout)
			if seen[ds] {
				continue
			}
			seen[ds] = true
			out = append(out, model.Holiday{
				Date:      date,
				Key:       key,
				NameEN:    nameEN,
				NameKM:    km,
				Conf:      model.ConfidenceComputed,
				Source:    w.Name(),
				SourceURL: "https://en.wikipedia.org/wiki/Public_holidays_in_Cambodia",
				UpdatedAt: now,
			})
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("wikipedia: parsed 0 holidays, article layout may have changed")
	}

	return &model.Snapshot{
		Year:      year,
		Source:    w.Name(),
		SourceURL: "https://en.wikipedia.org/wiki/Public_holidays_in_Cambodia",
		Holidays:  Normalize(out),
		FetchedAt: now,
	}, nil
}

// cleanWikitext strips links, refs, templates and markup from a table cell.
var (
	refRe      = regexp.MustCompile(`(?s)<ref[^>]*>.*?</ref>|<ref[^>]*/>`)
	tmplRe     = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	htmlTagRe  = regexp.MustCompile(`<[^>]+>`)
	pipeLinkRe = regexp.MustCompile(`\[\[(?:[^\]|]*\|)?([^\]]+)\]\]`)
)

func cleanWikitext(s string) string {
	s = refRe.ReplaceAllString(s, "")
	s = pipeLinkRe.ReplaceAllString(s, "$1")
	for range 3 { // templates can nest a couple of levels
		s = tmplRe.ReplaceAllString(s, "")
	}
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = strings.NewReplacer("'''", "", "''", "", "|", " ", "!", " ").Replace(s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func monthFromName(name string) (time.Month, bool) {
	months := map[string]time.Month{
		"january": time.January, "february": time.February, "march": time.March,
		"april": time.April, "may": time.May, "june": time.June,
		"july": time.July, "august": time.August, "september": time.September,
		"october": time.October, "november": time.November, "december": time.December,
	}
	m, ok := months[strings.ToLower(name)]
	return m, ok
}
