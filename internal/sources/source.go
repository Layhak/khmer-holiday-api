// Package sources implements the per-site adapters that supply holiday data.
package sources

import (
	"context"
	"errors"
	"sort"

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
	"wikipedia": 0,
	"nager":     1,
	"akp":       2,
	"mlvt":      3,
	"mef":       4,
}

// Precedence returns the tie-break weight for a source name.
func Precedence(name string) int { return precedence[name] }

// NewRegistry builds the default set of sources.
func NewRegistry(c *httpx.Client) *Registry {
	r := &Registry{sources: []Source{
		NewNager(c),     // computed  - always available, covers future years
		NewWikipedia(c), // computed  - cross-check for names and Khmer script
		NewAKP(c),       // reported  - state news agency announces the sub-decree
		NewMLVT(c),      // official  - Ministry of Labour publishes the Prakas
		NewMEF(c),       // official  - blocked today, kept for when it opens up
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
