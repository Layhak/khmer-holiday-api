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

// MLVT reads the Ministry of Labour and Vocational Training, which publishes
// the annual Prakas on paid public holidays - the operative document for every
// employer in Cambodia.
//
// This is the most authoritative source that is actually reachable by an
// automated client. mef.gov.kh sits behind Cloudflare and refuses us; MLVT does
// not. The Prakas is listed under ឯកសារផ្លូវការ/ប្រកាស with a title of the form
// "ប្រកាស លេខ២១៦/២៥ ស្តីពី ការឈប់បុណ្យដែលមានប្រាក់ឈ្នួល ប្រចាំឆ្នាំ២០២៦".
//
// The catch: the attached PDF is a SCANNED IMAGE with no text layer, so the
// dates cannot be extracted without OCR. This adapter therefore returns an
// evidence-only snapshot - the decree number and the PDF URL - which proves
// which document governs the year and gives an operator a direct link to
// verify against. Use `khapi verify` to promote a year to official once checked.
type MLVT struct{ c *httpx.Client }

// NewMLVT constructs the adapter.
func NewMLVT(c *httpx.Client) *MLVT { return &MLVT{c: c} }

func (m *MLVT) Name() string { return "mlvt" }

// Authority is official: it publishes the operative legal instrument.
func (m *MLVT) Authority() model.Confidence { return model.ConfidenceOfficial }

const (
	mlvtBase        = "https://www.mlvt.gov.kh"
	mlvtPrakasPath  = "/index.php/ឯកសារផ្លូវការ/ប្រកាស.html"
	mlvtAnukretPath = "/index.php/ឯកសារផ្លូវការ/អនុក្រឹត្យ-សេចក្ដីសម្រេច.html"
)

// khmerHolidayPhrase is "paid holiday leave" - the phrase that identifies the
// annual holiday Prakas among all the ministry's other Prakas documents.
const khmerHolidayPhrase = "ការឈប់បុណ្យ"

var (
	mlvtItemRe = regexp.MustCompile(`href="(/index\.php/[^"]*?/(\d+)\.html)"[^>]*>([^<]{10,300})<`)
	// The detail page hands the PDF to a JS viewer: $('#pdfviewer0').pdfViewer("https://.../x.pdf", ...)
	mlvtPDFRe = regexp.MustCompile(`pdfViewer\(\s*"([^"]+\.pdf)"`)
	// Fallback: any attachment PDF on the page.
	mlvtAnyPDFRe = regexp.MustCompile(`https?://[^"'\s]+/media/k2/attachments/[^"'\s]+\.pdf`)
	// "លេខ២១៦/២៥" - decree number in Khmer numerals.
	mlvtDecreeRe = regexp.MustCompile(`លេខ\s*([០-៩]+)\s*/\s*([០-៩]+)`)
	// "ប្រចាំឆ្នាំ២០២៦" - "for the year 2026", in Khmer or Arabic numerals.
	mlvtYearRe = regexp.MustCompile(`ប្រចាំឆ្នាំ\s*([០-៩]{4}|\d{4})`)
)

// mentionsYear reports whether the page states it covers the given year.
func mentionsYear(page string, year int) bool {
	want := fmt.Sprint(year)
	for _, m := range mlvtYearRe.FindAllStringSubmatch(page, -1) {
		if fromKhmerNumerals(m[1]) == want {
			return true
		}
	}
	return false
}

// Fetch finds the holiday Prakas for the year and records its provenance.
func (m *MLVT) Fetch(ctx context.Context, year int) (*model.Snapshot, error) {
	for _, path := range []string{mlvtPrakasPath, mlvtAnukretPath} {
		listURL := mlvtBase + path

		body, err := m.c.Get(ctx, listURL)
		if err != nil {
			if e := wrapStatus(err); Blocked(e) {
				return nil, e
			}
			continue
		}

		// Listing titles are truncated with an ellipsis, and the year sits
		// past the cut ("...ការឈប់បុណ្យដែលមានប្រាក់ឈ្ន..."). So the listing can
		// only shortlist candidates by the holiday phrase; the year has to be
		// confirmed on each candidate's detail page, which carries the full
		// title. Several years of the Prakas are listed together, so we check
		// candidates in order until one names the year we want.
		seen := map[string]bool{}
		for _, mm := range mlvtItemRe.FindAllStringSubmatch(string(body), -1) {
			href, title := mm[1], html2text(mm[3])

			if !strings.Contains(title, khmerHolidayPhrase) || seen[href] {
				continue
			}
			seen[href] = true

			detailURL := mlvtBase + href
			snap, err := m.readDetail(ctx, year, detailURL, title)
			if err != nil {
				if NotPublished(err) {
					continue // right kind of document, wrong year
				}
				return nil, err
			}
			return snap, nil
		}
	}

	return nil, fmt.Errorf("%w: mlvt has no %d holiday Prakas yet", ErrNotPublished, year)
}

// readDetail confirms the document is for the requested year, then pulls the
// PDF link and decree number off the page. It returns ErrNotPublished when the
// page turns out to cover a different year, so the caller can keep looking.
func (m *MLVT) readDetail(ctx context.Context, year int, url, listTitle string) (*model.Snapshot, error) {
	body, err := m.c.Get(ctx, url)
	if err != nil {
		return nil, wrapStatus(err)
	}
	page := string(body)

	// The detail page carries the untruncated title, e.g.
	// "...ការឈប់បុណ្យដែលមានប្រាក់ឈ្នួល ប្រចាំឆ្នាំ២០២៦" where ប្រចាំឆ្នាំ means
	// "for the year". Anchoring on that phrase avoids matching a stray "2026"
	// elsewhere in the page chrome.
	if !mentionsYear(page, year) {
		return nil, fmt.Errorf("%w: mlvt document %s is not for %d", ErrNotPublished, url, year)
	}

	pdf := ""
	if mm := mlvtPDFRe.FindStringSubmatch(page); mm != nil {
		pdf = mm[1]
	} else if mm := mlvtAnyPDFRe.FindString(page); mm != "" {
		pdf = mm
	}

	decree := ""
	if mm := mlvtDecreeRe.FindStringSubmatch(listTitle); mm != nil {
		decree = fmt.Sprintf("Prakas No. %s/%s",
			fromKhmerNumerals(mm[1]), fromKhmerNumerals(mm[2]))
	}

	return &model.Snapshot{
		Year:        year,
		Source:      m.Name(),
		SourceURL:   url,
		DocumentURL: pdf,
		Decree:      decree,
		Note: "Prakas located; PDF is a scanned image with no text layer, " +
			"so dates require manual or OCR verification. Title: " + listTitle,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// khmerDigits maps 0-9 to Khmer numerals ០-៩.
var khmerDigits = []rune{'០', '១', '២', '៣', '៤', '៥', '៦', '៧', '៨', '៩'}

func toKhmerNumerals(n int) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(khmerDigits[r-'0'])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fromKhmerNumerals(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '០' && r <= '៩' {
			b.WriteRune('0' + (r - '០'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
