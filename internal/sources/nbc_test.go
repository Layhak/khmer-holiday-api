package sources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
	"github.com/layhak/khmer-holiday-api/internal/model"
)

func newNBCTestSource(t *testing.T, payload string, status int) *NBC {
	t.Helper()

	client := httpx.New()
	client.HTTP = &http.Client{Transport: tallyfyRoundTripFunc(
		func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
				Body:       io.NopCloser(strings.NewReader(payload)),
				Request:    req,
			}, nil
		},
	)}
	client.Retries = 0
	client.MinInterval = 0
	return &NBC{c: client, endpoint: "https://nbc.test/holidays"}
}

func TestNBCFetchesOfficialMultiDayCalendar(t *testing.T) {
	page := `<html><head><title>Public Holidays 2026</title></head><body>
		<table><tr><td>Unclosed layout cell
		<table class="general-2">
		<tr><td width="150">01 Jan</td><td>International New Year Day</td></tr>
		<tr><td>07 Jan</td><td>Day of Victory over the Genocidal Regime</td></tr>
		<tr><td>08 Mar</td><td>International Women's Rights Day</td></tr>
		<tr><td>14-15-16 Apr</td><td>Khmer New Year's Day</td></tr>
		<tr><td>01 May</td><td>International Labor Day</td></tr>
		<tr><td>05 May</td><td>Royal Ploughing Ceremony</td></tr>
		<tr><td>14 May</td><td>Birthday of King NORODOM SIHAMONI</td></tr>
		<tr><td>18 Jun</td><td>Birthday of Her Majesty the Queen-Mother</td></tr>
		<tr><td>24 Sep</td><td>Constitution Day</td></tr>
		<tr><td>10-11-12 Oct</td><td>Pchum Ben Day</td></tr>
		<tr><td>15 Oct</td><td>Mourning Day of the Late King-Father NORODOM SIHANOUK</td></tr>
		<tr><td>29 Oct</td><td>Coronation Day of King NORODOM SIHAMONI</td></tr>
		<tr><td>09 Nov</td><td>National Independence Day</td></tr>
		<tr><td>23-24-25 Nov</td><td>Water Festival</td></tr>
		<tr><td>29 Dec</td><td>Peace Day in Cambodia</td></tr>
		</table></td></tr></table></body></html>`
	src := newNBCTestSource(t, page, http.StatusOK)

	snap, err := src.Fetch(context.Background(), 2026)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(src, snap, 2026); err != nil {
		t.Fatal(err)
	}
	if !snap.Complete || len(snap.Holidays) != 21 {
		t.Fatalf("complete/count = %v/%d, want true/21", snap.Complete, len(snap.Holidays))
	}
	for _, holiday := range snap.Holidays {
		if holiday.Conf != model.ConfidenceOfficial || holiday.Source != "nbc" {
			t.Fatalf("holiday confidence/source = %s/%s, want official/nbc",
				holiday.Conf, holiday.Source)
		}
	}
	if got := snap.Holidays[3]; got.Date.Format(model.DateLayout) != "2026-04-14" ||
		got.Key != "khmer_new_year" || got.Ordinal != 1 || got.OfDays != 3 {
		t.Fatalf("first Khmer New Year row = %+v", got)
	}
}

func TestNBCRejectsWrongYearAndPartialCalendar(t *testing.T) {
	wrongYear := newNBCTestSource(t,
		`<title>Public Holidays 2026</title><tr><td>01 Jan</td><td>New Year</td></tr>`,
		http.StatusOK)
	if _, err := wrongYear.Fetch(context.Background(), 2027); !errors.Is(err, ErrNotPublished) {
		t.Fatalf("wrong-year error = %v, want ErrNotPublished", err)
	}

	partial := newNBCTestSource(t,
		`<title>Public Holidays 2026</title><table class="general-2">`+
			`<tr><td>01 Jan</td><td>New Year</td></tr></table>`,
		http.StatusOK)
	if _, err := partial.Fetch(context.Background(), 2026); err == nil ||
		!strings.Contains(err.Error(), "implausible holiday count") {
		t.Fatalf("partial-page error = %v", err)
	}
}

func TestNBCMapsBlockedResponse(t *testing.T) {
	src := newNBCTestSource(t, `blocked`, http.StatusForbidden)
	if _, err := src.Fetch(context.Background(), 2026); !errors.Is(err, ErrBlocked) {
		t.Fatalf("error = %v, want ErrBlocked", err)
	}
}
