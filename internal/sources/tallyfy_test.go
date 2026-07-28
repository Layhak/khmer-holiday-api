package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
	"github.com/layhak/khmer-holiday-api/internal/model"
)

type tallyfyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f tallyfyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTallyfyTestSource(t *testing.T, payload string, status int) *Tallyfy {
	t.Helper()

	client := httpx.New()
	client.HTTP = &http.Client{Transport: tallyfyRoundTripFunc(
		func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(payload)),
				Request:    req,
			}, nil
		},
	)}
	client.Retries = 0
	client.MinInterval = 0
	return &Tallyfy{c: client, endpoint: "https://tallyfy.test/%d.json"}
}

func TestTallyfyFetchesNationalHolidaysOnly(t *testing.T) {
	payload := `{
		"country":{"code":"KH"},
		"year":2027,
		"holidays":[
			{
				"date":"2027-04-14",
				"name":"Khmer New Year Day 1",
				"local_name":"ចូលឆ្នាំខ្មែរ",
				"type":"national",
				"observed_date":"2027-04-15",
				"is_observed_shifted":true
			},
			{
				"date":"2027-04-16",
				"name":"Bank closure",
				"local_name":"",
				"type":"bank",
				"observed_date":"2027-04-16",
				"is_observed_shifted":false
			}
		]
	}`
	src := newTallyfyTestSource(t, payload, http.StatusOK)

	snap, err := src.Fetch(context.Background(), 2027)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(src, snap, 2027); err != nil {
		t.Fatal(err)
	}
	if snap.Complete {
		t.Fatal("Tallyfy cross-check must not authorize destructive replacement")
	}
	if len(snap.Holidays) != 1 {
		t.Fatalf("holiday count = %d, want one national row", len(snap.Holidays))
	}
	got := snap.Holidays[0]
	if got.Date.Format(model.DateLayout) != "2027-04-15" {
		t.Errorf("date = %s, want observed date 2027-04-15", got.Date.Format(model.DateLayout))
	}
	if got.Key != "khmer_new_year" || got.Conf != model.ConfidenceComputed {
		t.Errorf("holiday = %s/%s, want khmer_new_year/computed", got.Key, got.Conf)
	}
}

func TestTallyfyRejectsMismatchedMetadataAndBadDates(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "country",
			payload: `{"country":{"code":"TH"},"year":2027,"holidays":[]}`,
			want:    "is not KH",
		},
		{
			name:    "year",
			payload: `{"country":{"code":"KH"},"year":2028,"holidays":[]}`,
			want:    "does not match",
		},
		{
			name: "date",
			payload: `{"country":{"code":"KH"},"year":2027,"holidays":[` +
				`{"date":"not-a-date","name":"Holiday","type":"national"}]}`,
			want: "bad date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newTallyfyTestSource(t, tt.payload, http.StatusOK)
			if _, err := src.Fetch(context.Background(), 2027); err == nil ||
				!strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want text %q", err, tt.want)
			}
		})
	}
}

func TestTallyfyExcludesAmbiguousDuplicateDates(t *testing.T) {
	payload := `{
		"country":{"code":"KH"},
		"year":2027,
		"holidays":[
			{"date":"2027-01-01","name":"New Year's Day","type":"national"},
			{"date":"2027-05-13","name":"Visak Bochea Day","type":"national"},
			{"date":"2027-05-13","name":"King's Birthday Day 1","type":"national"}
		]
	}`
	src := newTallyfyTestSource(t, payload, http.StatusOK)

	snap, err := src.Fetch(context.Background(), 2027)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(src, snap, 2027); err != nil {
		t.Fatal(err)
	}
	if len(snap.Holidays) != 1 || snap.Holidays[0].Key != "intl_new_year" {
		t.Fatalf("holidays = %#v, want only the unambiguous New Year row", snap.Holidays)
	}
	if !strings.Contains(snap.Note, "excluded 1 date") {
		t.Errorf("note = %q, want collision disclosure", snap.Note)
	}
}

func TestTallyfyMapsMissingYearAndBlockedResponses(t *testing.T) {
	for _, tt := range []struct {
		status int
		want   error
	}{
		{http.StatusNotFound, ErrNotPublished},
		{http.StatusForbidden, ErrBlocked},
	} {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			src := newTallyfyTestSource(t, `{}`, tt.status)
			_, err := src.Fetch(context.Background(), time.Now().Year())
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}
