package sources

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"
)

type testSource struct {
	name string
	auth model.Confidence
}

func (s testSource) Name() string                { return s.name }
func (s testSource) Authority() model.Confidence { return s.auth }
func (s testSource) Fetch(context.Context, int) (*model.Snapshot, error) {
	return nil, nil
}

func validSnapshot() *model.Snapshot {
	return &model.Snapshot{
		Year:     2027,
		Source:   "test",
		Complete: true,
		Holidays: []model.Holiday{{
			Date:      time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
			Key:       "intl_new_year",
			NameEN:    "International New Year's Day",
			Conf:      model.ConfidenceComputed,
			Source:    "test",
			SourceURL: "https://example.test/holidays",
			UpdatedAt: time.Now().UTC(),
		}},
	}
}

func TestValidateSnapshotAcceptsValidCompleteYear(t *testing.T) {
	src := testSource{name: "test", auth: model.ConfidenceComputed}
	if err := ValidateSnapshot(src, validSnapshot(), 2027); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSnapshotRejectsCrossYearAndOverclaimedAuthority(t *testing.T) {
	src := testSource{name: "test", auth: model.ConfidenceComputed}

	crossYear := validSnapshot()
	crossYear.Holidays[0].Date = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := ValidateSnapshot(src, crossYear, 2027); err == nil ||
		!strings.Contains(err.Error(), "outside requested year") {
		t.Fatalf("cross-year error = %v", err)
	}

	overclaim := validSnapshot()
	overclaim.Holidays[0].Conf = model.ConfidenceOfficial
	if err := ValidateSnapshot(src, overclaim, 2027); err == nil ||
		!strings.Contains(err.Error(), "above source authority") {
		t.Fatalf("authority error = %v", err)
	}
}

func TestValidateSnapshotRejectsUnsafeProvenanceURL(t *testing.T) {
	src := testSource{name: "test", auth: model.ConfidenceComputed}
	snap := validSnapshot()
	snap.Holidays[0].SourceURL = "javascript:alert(1)"

	if err := ValidateSnapshot(src, snap, 2027); err == nil ||
		!strings.Contains(err.Error(), "absolute HTTP(S)") {
		t.Fatalf("URL error = %v", err)
	}
}

func TestValidateDocumentReferenceRejectsUnsafeInput(t *testing.T) {
	if err := ValidateDocumentReference("Prakas No. 1", "javascript:alert(1)"); err == nil {
		t.Fatal("unsafe document URL was accepted")
	}
	if err := ValidateDocumentReference("Prakas No. 1\r\nInjected: value", ""); err == nil {
		t.Fatal("control characters in decree reference were accepted")
	}
}
