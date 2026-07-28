package sources

import (
	"context"
	"errors"
	"testing"

	"github.com/layhak/khmer-holiday-api/internal/model"
)

func TestVerifiedArchiveReturnsCompleteOfficial2025Calendar(t *testing.T) {
	src := NewVerifiedArchive()
	snap, err := src.Fetch(context.Background(), 2025)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(src, snap, 2025); err != nil {
		t.Fatal(err)
	}
	if !snap.Complete || len(snap.Holidays) != 22 {
		t.Fatalf("complete/count = %v/%d, want true/22", snap.Complete, len(snap.Holidays))
	}
	foundVisak := false
	for _, holiday := range snap.Holidays {
		if holiday.Conf != model.ConfidenceOfficial {
			t.Fatalf("confidence = %s, want official", holiday.Conf)
		}
		if holiday.Date.Format(model.DateLayout) == "2025-05-11" &&
			holiday.Key == "visak_bochea" {
			foundVisak = true
		}
	}
	if !foundVisak {
		t.Fatal("verified 2025 calendar is missing Visak Bochea on 11 May")
	}
}

func TestVerifiedArchiveDoesNotInventUnverifiedYears(t *testing.T) {
	_, err := NewVerifiedArchive().Fetch(context.Background(), 2024)
	if !errors.Is(err, ErrNotPublished) {
		t.Fatalf("error = %v, want ErrNotPublished", err)
	}
}
