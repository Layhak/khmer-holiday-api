// Package sources implements the per-site adapters that supply holiday data.
package sources

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
	"github.com/layhak/khmer-holiday-api/internal/model"
)

// ErrNotPublished means the source is reachable but has no data for the year
// yet - the normal state for next year's sub-decree before ~September.
var ErrNotPublished = errors.New("source has not published this year yet")

// ErrBlocked means the source refused an automated client (Cloudflare 403).
var ErrBlocked = errors.New("source blocked the request")

// Source fetches the holiday list for one year from one place.
type Source interface {
	// Name is the stable identifier used in the CLI and the audit table.
	Name() string

	// Authority is the best confidence this source can produce. It bounds
	// the records the adapter emits, so a news site can never mint an
	// "official" date.
	Authority() model.Confidence

	// Fetch returns the year's holidays, or ErrNotPublished / ErrBlocked.
	Fetch(ctx context.Context, year int) (*model.Snapshot, error)
}

// ValidateSnapshot enforces the trust boundary between an upstream parser and
// reconciliation. A reachable source is not automatically a valid source: a
// changed page, captive portal, or compromised response must not be allowed to
// inject cross-year, duplicate, over-privileged, or unsafe records.
func ValidateSnapshot(src Source, snap *model.Snapshot, year int) error {
	if snap == nil {
		return fmt.Errorf("%s: returned a nil snapshot", src.Name())
	}
	if snap.Year != year {
		return fmt.Errorf("%s: snapshot year %d does not match requested year %d",
			src.Name(), snap.Year, year)
	}
	if snap.Source != src.Name() {
		return fmt.Errorf("%s: snapshot identifies source as %q", src.Name(), snap.Source)
	}
	if len(snap.Holidays) > 366 {
		return fmt.Errorf("%s: returned %d holiday rows; maximum is 366",
			src.Name(), len(snap.Holidays))
	}
	if snap.Complete && len(snap.Holidays) == 0 {
		return fmt.Errorf("%s: marked an empty snapshot complete", src.Name())
	}
	if snap.AnnouncedDays < 0 || snap.AnnouncedDays > 366 {
		return fmt.Errorf("%s: implausible announced day count %d",
			src.Name(), snap.AnnouncedDays)
	}
	for label, raw := range map[string]string{
		"source URL":   snap.SourceURL,
		"document URL": snap.DocumentURL,
	} {
		if err := validatePublicURL(raw); err != nil {
			return fmt.Errorf("%s: invalid %s: %w", src.Name(), label, err)
		}
	}
	if len(snap.Decree) > 200 || len(snap.Note) > 2000 {
		return fmt.Errorf("%s: snapshot metadata is unreasonably long", src.Name())
	}

	seen := make(map[string]struct{}, len(snap.Holidays))
	for i, h := range snap.Holidays {
		if err := h.Validate(); err != nil {
			return fmt.Errorf("%s: row %d: %w", src.Name(), i+1, err)
		}
		if h.Date.Year() != year {
			return fmt.Errorf("%s: row %d has date %s outside requested year %d",
				src.Name(), i+1, h.Date.Format(model.DateLayout), year)
		}
		if h.Source != src.Name() {
			return fmt.Errorf("%s: row %d identifies source as %q",
				src.Name(), i+1, h.Source)
		}
		if h.Conf.Rank() > src.Authority().Rank() {
			return fmt.Errorf("%s: row %d claims %q confidence above source authority %q",
				src.Name(), i+1, h.Conf, src.Authority())
		}
		if err := validatePublicURL(h.SourceURL); err != nil {
			return fmt.Errorf("%s: row %d has invalid source URL: %w", src.Name(), i+1, err)
		}
		key := h.Date.Format(model.DateLayout)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%s: duplicate holiday date %s", src.Name(), key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validatePublicURL(raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > 2048 {
		return fmt.Errorf("URL exceeds 2048 bytes")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return fmt.Errorf("must be an absolute HTTP(S) URL")
	}
	if u.User != nil {
		return fmt.Errorf("embedded credentials are not allowed")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return fmt.Errorf("control characters are not allowed")
	}
	return nil
}

// ValidateDocumentReference checks operator-supplied provenance before it is
// exposed through the public API by the verify command.
func ValidateDocumentReference(decree, rawURL string) error {
	decree = strings.TrimSpace(decree)
	if decree == "" {
		return fmt.Errorf("decree reference is required")
	}
	if len(decree) > 200 || strings.ContainsAny(decree, "\r\n") {
		return fmt.Errorf("decree reference is invalid or too long")
	}
	if err := validatePublicURL(strings.TrimSpace(rawURL)); err != nil {
		return fmt.Errorf("document URL: %w", err)
	}
	return nil
}

// Registry holds the configured sources, ordered weakest to strongest so that
// a sequential scrape naturally lets better data land last.
type Registry struct {
	sources []Source
}

// precedence breaks ties between sources of equal authority. Nager and
// Wikipedia are both "computed", but Nager tracks the sub-decree year by year
// while Wikipedia only carries the fixed-date holidays as general reference -
// so Nager's dates must win where the two overlap.
var precedence = map[string]int{
	"tallyfy":       -1,
	"wikipedia":     0,
	"nager":         1,
	"akp":           2,
	"mlvt":          3,
	"mlvt_verified": 4,
	"nbc":           5,
	"mef":           6,
}

// Precedence returns the tie-break weight for a source name.
func Precedence(name string) int { return precedence[name] }

// NewRegistry builds the default set of sources.
func NewRegistry(c *httpx.Client) *Registry {
	r := &Registry{sources: []Source{
		NewTallyfy(c),        // computed  - lowest-precedence future-year cross-check
		NewNager(c),          // computed  - always available, covers future years
		NewWikipedia(c),      // computed  - cross-check for names and Khmer script
		NewAKP(c),            // reported  - state news agency announces the sub-decree
		NewMLVT(c),           // official  - Ministry of Labour publishes the Prakas
		NewVerifiedArchive(), // official  - checked past-year MLVT calendars
		NewNBC(c),            // official  - government calendar with machine-readable dates
		NewMEF(c),            // official  - blocked today, kept for when it opens up
	}}
	sort.SliceStable(r.sources, func(i, j int) bool {
		a, b := r.sources[i], r.sources[j]
		if a.Authority().Rank() != b.Authority().Rank() {
			return a.Authority().Rank() < b.Authority().Rank()
		}
		return Precedence(a.Name()) < Precedence(b.Name())
	})
	return r
}

// All returns every source, weakest authority first.
func (r *Registry) All() []Source { return r.sources }

// Names lists the registered source names.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, s.Name())
	}
	return out
}

// Get looks up a single source by name.
func (r *Registry) Get(name string) (Source, bool) {
	for _, s := range r.sources {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}
