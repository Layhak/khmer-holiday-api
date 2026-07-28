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

// AKP reads Agence Kampuchea Presse, the Cambodian state news agency.
//
// AKP announces the holiday sub-decree within a day or two of the Prime
// Minister signing it - the 2026 schedule appeared as "Cambodia Announces 21
// Days of Public Holidays for 2026" on 5 September 2025. The article gives the
// total day count and the decree context but does not enumerate every date, so
// this adapter returns an evidence-only snapshot: no holiday rows, but an
// authoritative day count to validate the dates other sources supply.
//
// AKP has no working search endpoint (it 500s), so the adapter pages through
// the national news category listings instead.
type AKP struct{ c *httpx.Client }

// NewAKP constructs the adapter.
func NewAKP(c *httpx.Client) *AKP { return &AKP{c: c} }

func (a *AKP) Name() string { return "akp" }

// Authority is reported: AKP is a government outlet relaying the sub-decree,
// but it is journalism, not the instrument itself.
func (a *AKP) Authority() model.Confidence { return model.ConfidenceReported }

// akpCategories are the listing IDs that carry national news. Category 7 has
// carried every holiday announcement observed so far; 8 is a fallback.
var akpCategories = []int{7, 8}

// The category listing is strictly reverse-chronological with ~10 articles per
// page, so the announcement for year N - published around September of N-1 -
// sits several hundred pages deep. Crawling from page 1 would never reach it,
// so the adapter binary-searches the pagination by article date to the start of
// the announcement window, then scans that window linearly.
//
// Observed: the "21 Days of Public Holidays for 2026" article, published
// 5 September 2025, sat on page 410 of category 7 in July 2026.
const (
	akpMaxPage  = 900 // hard ceiling for the binary search
	akpScanSpan = 140 // pages scanned from the window start, ~mid-Nov back to ~Aug
)

var (
	akpLinkRe  = regexp.MustCompile(`href="(https://akp\.gov\.kh/post/detail/\d+)"[^>]*>([^<]{10,200})<`)
	akpDaysRe  = regexp.MustCompile(`(?i)\b(\d{1,3})\s+days?\s+of\s+(?:official\s+)?public\s+holidays?`)
	akpTotalRe = regexp.MustCompile(`(?i)total\s+of\s+(\d{1,3})\s+days`)
	akpTagRe   = regexp.MustCompile(`<[^>]+>`)
)

// Fetch locates the announcement article for the year and extracts the day count.
func (a *AKP) Fetch(ctx context.Context, year int) (*model.Snapshot, error) {
	articleURL, title, err := a.findAnnouncement(ctx, year)
	if err != nil {
		return nil, err
	}

	body, err := a.c.Get(ctx, articleURL)
	if err != nil {
		return nil, wrapStatus(err)
	}
	text := stripHTML(string(body))

	days := 0
	if m := akpDaysRe.FindStringSubmatch(text); m != nil {
		days, _ = strconv.Atoi(m[1])
	} else if m := akpTotalRe.FindStringSubmatch(text); m != nil {
		days, _ = strconv.Atoi(m[1])
	} else if m := akpDaysRe.FindStringSubmatch(title); m != nil {
		days, _ = strconv.Atoi(m[1])
	}
	if days <= 0 || days > 60 {
		return nil, fmt.Errorf("akp: found article %q but no plausible day count", articleURL)
	}

	return &model.Snapshot{
		Year:          year,
		Source:        a.Name(),
		SourceURL:     articleURL,
		AnnouncedDays: days,
		Decree:        findDecree(text),
		Note:          strings.TrimSpace(title),
		FetchedAt:     time.Now().UTC(),
	}, nil
}

// findAnnouncement locates the headline announcing the year's holiday schedule.
//
// The sub-decree is normally signed between August and November of the previous
// year, so the search starts at the page covering 1 December of year-1 (safely
// after any plausible announcement) and scans backwards in time from there.
func (a *AKP) findAnnouncement(ctx context.Context, year int) (url, title string, err error) {
	windowEnd := time.Date(year-1, time.November, 15, 0, 0, 0, 0, time.UTC)

	for _, cat := range akpCategories {
		start, ferr := a.pageForDate(ctx, cat, windowEnd)
		if ferr != nil {
			if Blocked(ferr) {
				return "", "", ferr
			}
			continue
		}

		for page := start; page < start+akpScanSpan && page <= akpMaxPage; page++ {
			body, ferr := a.listPage(ctx, cat, page)
			if ferr != nil {
				if Blocked(ferr) {
					return "", "", ferr
				}
				break
			}
			if href, text, ok := matchAnnouncement(string(body), year); ok {
				return href, text, nil
			}
		}
	}

	return "", "", fmt.Errorf("%w: akp has no %d holiday announcement yet", ErrNotPublished, year)
}

// matchAnnouncement scans one listing page for the announcement headline.
func matchAnnouncement(page string, year int) (href, title string, ok bool) {
	ys := strconv.Itoa(year)

	for _, m := range akpLinkRe.FindAllStringSubmatch(page, -1) {
		link, text := m[1], strings.TrimSpace(html2text(m[2]))
		low := strings.ToLower(text)

		if !strings.Contains(low, "holiday") || !strings.Contains(text, ys) {
			continue
		}
		// Skip retrospective or logistical coverage ("traffic during the
		// holiday"); we want the schedule announcement itself.
		if strings.Contains(low, "during") || strings.Contains(low, "ahead of") ||
			strings.Contains(low, "travel") || strings.Contains(low, "traffic") {
			continue
		}
		return link, text, true
	}
	return "", "", false
}

// pageForDate binary-searches the pagination for the first page whose newest
// article is at or older than target. Pages are ordered newest first, so page
// number increases as dates decrease - a monotonic key we can bisect on.
func (a *AKP) pageForDate(ctx context.Context, cat int, target time.Time) (int, error) {
	lo, hi := 1, akpMaxPage
	best := 1

	for lo <= hi {
		mid := (lo + hi) / 2

		body, err := a.listPage(ctx, cat, mid)
		if err != nil {
			if Blocked(err) {
				return 0, err
			}
			// Past the end of the listing: search the newer half.
			hi = mid - 1
			continue
		}

		newest, ok := newestDate(string(body))
		if !ok {
			hi = mid - 1
			continue
		}

		if newest.After(target) {
			lo = mid + 1 // still too recent, go deeper
		} else {
			best = mid
			hi = mid - 1 // far enough back, try to get closer
		}
	}
	return best, nil
}

func (a *AKP) listPage(ctx context.Context, cat, page int) ([]byte, error) {
	url := fmt.Sprintf("https://akp.gov.kh/post/category/%d?page=%d", cat, page)
	body, err := a.c.Get(ctx, url)
	if err != nil {
		return nil, wrapStatus(err)
	}
	return body, nil
}

// akpDatelineRe matches the AKP dateline, e.g. "AKP Phnom Penh, July 28, 2026".
var akpDatelineRe = regexp.MustCompile(`AKP\s+[A-Za-z ]+,\s*([A-Z][a-z]+\s+\d{1,2},\s*\d{4})`)

// newestDate returns the most recent plausible dateline on a listing page.
//
// AKP's own copy contains occasional typos - a real article on page 410 is
// datelined "September 05, 2925". Because the binary search treats the page's
// date as a monotonic key, one such outlier would push every comparison the
// wrong way and send the search to the end of the listing. Dates outside a
// sane window are therefore discarded rather than merely sorted.
func newestDate(page string) (time.Time, bool) {
	var newest time.Time

	minYear := 2000
	maxYear := time.Now().UTC().Year() + 1

	for _, m := range akpDatelineRe.FindAllStringSubmatch(html2text(page), -1) {
		raw := strings.Join(strings.Fields(m[1]), " ")
		for _, layout := range []string{"January 2, 2006", "January 02, 2006"} {
			t, err := time.Parse(layout, raw)
			if err != nil {
				continue
			}
			if t.Year() < minYear || t.Year() > maxYear {
				break // typo, e.g. "2925"
			}
			if t.After(newest) {
				newest = t
			}
			break
		}
	}
	return newest, !newest.IsZero()
}

// decreeRe matches "Sub-Decree No. 167" and similar references.
var decreeRe = regexp.MustCompile(`(?i)sub[-\s]?decree\s*(?:no\.?|number)?\s*(\d{1,4})`)

func findDecree(text string) string {
	if m := decreeRe.FindStringSubmatch(text); m != nil {
		return "Sub-Decree No. " + m[1]
	}
	return ""
}

// Go's regexp is RE2 and has no backreferences, so script and style blocks get
// one pattern each rather than a captured tag name.
var (
	scriptRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
)

func stripHTML(s string) string {
	s = scriptRe.ReplaceAllString(s, " ")
	s = styleRe.ReplaceAllString(s, " ")
	return html2text(akpTagRe.ReplaceAllString(s, " "))
}

func html2text(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
		"&#39;", "'", "&nbsp;", " ", "&rsquo;", "'", "&ldquo;", `"`, "&rdquo;", `"`,
	)
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}
