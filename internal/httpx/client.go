// Package httpx provides the shared HTTP client used by every source adapter.
package httpx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BrowserUA is sent on every request. Several Cambodian government sites sit
// behind Cloudflare and reject clients that advertise a bot user-agent; a
// realistic UA is the difference between 200 and 403 on some of them.
const BrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// Client is a polite HTTP client: bounded timeout, retries on transient
// failures, and a delay between attempts so we never hammer a ministry site.
type Client struct {
	HTTP    *http.Client
	Retries int
	Backoff time.Duration
}

// New returns a Client with sensible defaults.
func New() *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
		Retries: 3,
		Backoff: 2 * time.Second,
	}
}

// StatusError reports a non-2xx response. Adapters inspect Code to tell a
// blocked source (403) apart from one that simply has not published yet (404).
type StatusError struct {
	URL  string
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d", e.URL, e.Code)
}

// Blocked reports whether the status suggests bot protection rather than a
// missing document.
func (e *StatusError) Blocked() bool {
	return e.Code == http.StatusForbidden || e.Code == http.StatusTooManyRequests
}

// Get fetches url and returns the body, retrying on 5xx and network errors.
// 4xx responses are returned immediately - retrying a 403 will not help.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.Backoff * time.Duration(attempt)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("build request for %s: %w", url, err)
		}
		req.Header.Set("User-Agent", BrowserUA)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9,km;q=0.8")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("GET %s: %w", url, err)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			se := &StatusError{URL: url, Code: resp.StatusCode}
			if resp.StatusCode < 500 {
				return nil, se // client errors are not retryable
			}
			lastErr = se
			continue
		}
		if readErr != nil {
			lastErr = fmt.Errorf("read %s: %w", url, readErr)
			continue
		}
		return body, nil
	}

	return nil, lastErr
}
