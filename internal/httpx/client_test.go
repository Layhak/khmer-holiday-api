package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(fn roundTripFunc) *Client {
	c := New()
	c.HTTP = &http.Client{Transport: fn}
	c.Retries = 0
	c.Backoff = 0
	c.MinInterval = 0
	return c
}

func response(r *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func TestGetRejectsOversizedResponse(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return response(r, http.StatusOK, "12345"), nil
	})
	c.MaxBodyBytes = 4

	_, err := c.Get(context.Background(), "https://example.test")
	if err == nil || !strings.Contains(err.Error(), "exceeds 4-byte limit") {
		t.Fatalf("Get error = %v, want response-size error", err)
	}
}

func TestGetRejectsNon2xxResponse(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return response(r, http.StatusMultipleChoices, ""), nil
	})

	_, err := c.Get(context.Background(), "https://example.test")
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusMultipleChoices {
		t.Fatalf("Get error = %v, want StatusError 300", err)
	}
}

func TestGetRetriesServerErrors(t *testing.T) {
	var attempts atomic.Int32
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			return response(r, http.StatusServiceUnavailable, "temporary"), nil
		}
		return response(r, http.StatusOK, "ok"), nil
	})
	c.Retries = 1

	got, err := c.Get(context.Background(), "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" || attempts.Load() != 2 {
		t.Fatalf("body/attempts = %q/%d, want ok/2", got, attempts.Load())
	}
}
