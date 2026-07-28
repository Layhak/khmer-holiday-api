// Package httpx provides the shared HTTP client used by every source adapter.
package httpx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BrowserUA is sent on every request. Several Cambodian government sites sit
// behind Cloudflare and reject clients that advertise a bot user-agent; a
// realistic UA is the difference between 200 and 403 on some of them.
const BrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

const defaultMaxBodyBytes int64 = 16 << 20

var errResponseTooLarge = errors.New("response body too large")

// Client is a polite HTTP client: bounded timeout, retries on transient
// failures, bounded response bodies, and pacing so we never hammer a ministry
// site.
type Client struct {
	HTTP         *http.Client
	Retries      int
	Backoff      time.Duration
	MinInterval  time.Duration
	MaxBodyBytes int64

	mu          sync.Mutex
	lastRequest time.Time
}

// New returns a Client with sensible defaults.
func New() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 20
	transport.MaxIdleConnsPerHost = 2
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 20 * time.Second
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}

	return &Client{
		HTTP: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
					return fmt.Errorf("refusing HTTPS downgrade redirect to %s", req.URL.Redacted())
				}
				return nil
			},
		},
		Retries:      3,
		Backoff:      2 * time.Second,
		MinInterval:  200 * time.Millisecond,
		MaxBodyBytes: defaultMaxBodyBytes,
	}
}

// StatusError reports a non-2xx response. Adapters inspect Code to tell a
// blocked source (403) apart from one that simply has not published yet (404).
type StatusError struct {
	URL        string
	Code       int
	RetryAfter time.Duration
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
// Other non-2xx responses are returned immediately. Response bodies larger
// than MaxBodyBytes are rejected rather than silently truncated.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			if err := waitContext(ctx, c.retryDelay(attempt)); err != nil {
				return nil, err
			}
		}
		if err := c.waitForTurn(ctx); err != nil {
			return nil, err
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

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			se := &StatusError{
				URL:        resp.Request.URL.Redacted(),
				Code:       resp.StatusCode,
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil, se
			}
			lastErr = se
			continue
		}

		body, readErr := readBody(resp.Body, c.maxBodyBytes())
		closeErr := resp.Body.Close()
		if readErr != nil {
			if errors.Is(readErr, errResponseTooLarge) {
				return nil, fmt.Errorf("read %s: %w", url, readErr)
			}
			lastErr = fmt.Errorf("read %s: %w", url, readErr)
			continue
		}
		if closeErr != nil {
			lastErr = fmt.Errorf("close response from %s: %w", url, closeErr)
			continue
		}
		return body, nil
	}

	return nil, lastErr
}

func (c *Client) maxBodyBytes() int64 {
	if c.MaxBodyBytes <= 0 {
		return defaultMaxBodyBytes
	}
	return c.MaxBodyBytes
}

func (c *Client) retryDelay(attempt int) time.Duration {
	if c.Backoff <= 0 {
		return 0
	}
	delay := c.Backoff
	for i := 1; i < attempt && delay < 15*time.Second; i++ {
		delay *= 2
	}
	if delay > 15*time.Second {
		delay = 15 * time.Second
	}
	// A small jitter prevents scheduled scraper instances from retrying in
	// lockstep when an upstream briefly fails.
	return delay + time.Duration(rand.Int64N(int64(delay/4)+1))
}

func (c *Client) waitForTurn(ctx context.Context) error {
	if c.MinInterval <= 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	wait := time.Until(c.lastRequest.Add(c.MinInterval))
	if wait > 0 {
		if err := waitContext(ctx, wait); err != nil {
			return err
		}
	}
	c.lastRequest = time.Now()
	return nil
}

func waitContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func readBody(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: response exceeds %d-byte limit", errResponseTooLarge, limit)
	}
	return body, nil
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(raw); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
