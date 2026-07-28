package sources

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
	"github.com/layhak/khmer-holiday-api/internal/model"
)

// MEF reads the Ministry of Economy and Finance, mef.gov.kh.
//
// STATUS: blocked. As of this writing mef.gov.kh returns HTTP 403 to every
// non-browser client - it is behind Cloudflare bot protection, and a realistic
// User-Agent alone does not get past it. Verified against both the homepage and
// the document-category paths.
//
// The adapter is kept in the registry deliberately rather than deleted:
//
//  1. It records the block in the audit table on every run, so `khapi status`
//     shows whether the site has opened up rather than requiring someone to
//     remember to re-test it by hand.
//  2. If MEF drops the protection, this adapter starts working with no code
//     change.
//  3. If you need it sooner, point MEF_FETCH_CMD at a headless-browser helper
//     (see below) and the adapter will shell out to it instead.
//
// To use a headless fetcher, set MEF_FETCH_CMD to a command that takes a URL as
// its final argument and prints the page HTML on stdout, e.g.
//
//	MEF_FETCH_CMD="curl-impersonate-chrome -sL"
//
// Anything that defeats the bot check works; the parsing below is unchanged.
type MEF struct{ c *httpx.Client }

// NewMEF constructs the adapter.
func NewMEF(c *httpx.Client) *MEF { return &MEF{c: c} }

func (m *MEF) Name() string { return "mef" }

// Authority is official: MEF publishes the sub-decree for the public sector.
func (m *MEF) Authority() model.Confidence { return model.ConfidenceOfficial }

const mefBase = "https://mef.gov.kh"

var mefLinkRe = regexp.MustCompile(`href="(https?://mef\.gov\.kh/[^"]+)"[^>]*>([^<]{10,300})<`)

// Fetch attempts the ministry's document listings.
func (m *MEF) Fetch(ctx context.Context, year int) (*model.Snapshot, error) {
	khYear := toKhmerNumerals(year)

	paths := []string{
		"/documents-category/anukret",
		"/documents-category/prakas",
		"/",
	}

	var lastErr error
	for _, p := range paths {
		url := mefBase + p

		body, err := m.fetchPage(ctx, url)
		if err != nil {
			lastErr = err
			if Blocked(err) {
				continue
			}
			continue
		}

		for _, mm := range mefLinkRe.FindAllStringSubmatch(string(body), -1) {
			href, title := mm[1], html2text(mm[2])
			if !strings.Contains(title, khmerHolidayPhrase) && !strings.Contains(strings.ToLower(title), "holiday") {
				continue
			}
			if !strings.Contains(title, khYear) && !strings.Contains(title, fmt.Sprint(year)) {
				continue
			}
			return &model.Snapshot{
				Year:        year,
				Source:      m.Name(),
				SourceURL:   url,
				DocumentURL: href,
				Note:        "MEF document located: " + title,
				FetchedAt:   time.Now().UTC(),
			}, nil
		}
	}

	if lastErr != nil && Blocked(lastErr) {
		return nil, fmt.Errorf("%w: mef.gov.kh is behind Cloudflare bot protection "+
			"(HTTP 403); set MEF_FETCH_CMD to a headless-browser fetcher to bypass", ErrBlocked)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%w: mef has no %d holiday document", ErrNotPublished, year)
}

// fetchPage uses MEF_FETCH_CMD when configured, otherwise a plain HTTP GET.
func (m *MEF) fetchPage(ctx context.Context, url string) ([]byte, error) {
	if cmd := strings.TrimSpace(getenv("MEF_FETCH_CMD")); cmd != "" {
		return runFetchCmd(ctx, cmd, url)
	}
	body, err := m.c.Get(ctx, url)
	if err != nil {
		return nil, wrapStatus(err)
	}
	return body, nil
}

var _ = httpx.BrowserUA // keep the import meaningful if the client path changes
