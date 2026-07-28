package sources

import (
	"errors"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
)

// asStatusError unwraps err looking for an *httpx.StatusError.
func asStatusError(err error, target **httpx.StatusError) bool {
	return errors.As(err, target)
}

// Blocked reports whether err means the site refused an automated client.
func Blocked(err error) bool { return errors.Is(err, ErrBlocked) }

// NotPublished reports whether err means the year is not out yet.
func NotPublished(err error) bool { return errors.Is(err, ErrNotPublished) }

// Expected reports whether err is a normal, non-fatal outcome for a scrape:
// the source is blocked, or has not published the year yet. The CLI records
// these but does not treat them as failures.
func Expected(err error) bool { return Blocked(err) || NotPublished(err) }
