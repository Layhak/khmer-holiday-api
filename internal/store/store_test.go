package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func mk(y int, m time.Month, d int, key, source string, conf model.Confidence) model.Holiday {
	return model.Holiday{
		Date:      time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Key:       key,
		NameEN:    key,
		Conf:      conf,
		Source:    source,
		UpdatedAt: time.Now().UTC(),
	}
}

// The central guarantee: once a date is official, a later computed projection
// must not silently overwrite it.
func TestUpsertNeverDowngradesConfidence(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	official := mk(2027, time.October, 1, "pchum_ben", "mlvt", model.ConfidenceOfficial)
	if _, _, _, err := st.Upsert(ctx, []model.Holiday{official}); err != nil {
		t.Fatalf("upsert official: %v", err)
	}

	computed := mk(2027, time.October, 1, "pchum_ben", "nager", model.ConfidenceComputed)
	_, _, skipped, err := st.Upsert(ctx, []model.Holiday{computed})
	if err != nil {
		t.Fatalf("upsert computed: %v", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (the downgrade must be rejected)", skipped)
	}

	got, err := st.List(ctx, Filter{Year: 2027})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Conf != model.ConfidenceOfficial || got[0].Source != "mlvt" {
		t.Errorf("stored row = %s/%s, want official/mlvt", got[0].Conf, got[0].Source)
	}
}

func TestUpsertAcceptsUpgrade(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	if _, _, _, err := st.Upsert(ctx, []model.Holiday{
		mk(2027, time.October, 1, "pchum_ben", "nager", model.ConfidenceComputed),
	}); err != nil {
		t.Fatal(err)
	}
	_, updated, _, err := st.Upsert(ctx, []model.Holiday{
		mk(2027, time.October, 1, "pchum_ben", "mlvt", model.ConfidenceOfficial),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Errorf("updated = %d, want 1", updated)
	}

	got, _ := st.List(ctx, Filter{Year: 2027})
	if got[0].Conf != model.ConfidenceOfficial {
		t.Errorf("confidence = %q, want official after upgrade", got[0].Conf)
	}
}

func TestFilterByDayMonthYear(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	seed := []model.Holiday{
		mk(2026, time.January, 1, "intl_new_year", "nager", model.ConfidenceReported),
		mk(2026, time.April, 14, "khmer_new_year", "nager", model.ConfidenceReported),
		mk(2026, time.April, 15, "khmer_new_year", "nager", model.ConfidenceComputed),
		mk(2027, time.April, 14, "khmer_new_year", "nager", model.ConfidenceComputed),
	}
	if _, _, _, err := st.Upsert(ctx, seed); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		f    Filter
		want int
	}{
		{"year", Filter{Year: 2026}, 3},
		{"year+month", Filter{Year: 2026, Month: 4}, 2},
		{"year+month+day", Filter{Year: 2026, Month: 4, Day: 14}, 1},
		{"day across years", Filter{Month: 4, Day: 14}, 2},
		{"key", Filter{Key: "khmer_new_year"}, 3},
		{"official only", Filter{Year: 2026, OfficialOnly: true}, 0},
		{"range", Filter{From: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			To: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)}, 2},
		{"no filter", Filter{}, 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := st.List(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Errorf("got %d holidays, want %d", len(got), tc.want)
			}
		})
	}
}

func TestFilterValidationRejectsImpossibleValues(t *testing.T) {
	for _, f := range []Filter{
		{Month: 13},
		{Month: -1},
		{Day: 32},
		{From: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			To: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		if err := f.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want an error", f)
		}
	}
}

func TestStatusMarksProvisionalYears(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	if _, _, _, err := st.Upsert(ctx, []model.Holiday{
		mk(2026, time.January, 1, "intl_new_year", "mlvt", model.ConfidenceOfficial),
		mk(2027, time.January, 1, "intl_new_year", "nager", model.ConfidenceComputed),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d years, want 2", len(got))
	}
	if got[0].Provisional {
		t.Error("2026 is fully official; Provisional should be false")
	}
	if !got[1].Provisional {
		t.Error("2027 is computed only; Provisional should be true")
	}
}

func TestUpsertRejectsInvalidHoliday(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	bad := model.Holiday{Date: time.Now(), Key: "", NameEN: "x",
		Conf: model.ConfidenceComputed, Source: "s"}
	if _, _, _, err := st.Upsert(ctx, []model.Holiday{bad}); err == nil {
		t.Error("want an error for a holiday with an empty key, got nil")
	}

	unsafeURL := mk(2027, time.January, 1, "intl_new_year", "s", model.ConfidenceComputed)
	unsafeURL.SourceURL = "javascript:alert(1)"
	if _, _, _, err := st.Upsert(ctx, []model.Holiday{unsafeURL}); err == nil {
		t.Error("want an error for an unsafe source URL, got nil")
	}
}

func TestReplaceYearIsAtomicOnValidationFailure(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	original := mk(2027, time.January, 1, "intl_new_year", "nager", model.ConfidenceComputed)
	if _, _, _, err := st.Upsert(ctx, []model.Holiday{original}); err != nil {
		t.Fatal(err)
	}

	bad := mk(2026, time.January, 1, "wrong_year", "nager", model.ConfidenceComputed)
	if _, _, err := st.ReplaceYear(ctx, 2027, []model.Holiday{bad}); err == nil {
		t.Fatal("ReplaceYear accepted a row from the wrong year")
	}

	got, err := st.List(ctx, Filter{Year: 2027})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "intl_new_year" {
		t.Fatalf("existing year changed after failed replacement: %+v", got)
	}
}

func TestReplaceYearRemovesStaleDates(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	if _, _, _, err := st.Upsert(ctx, []model.Holiday{
		mk(2027, time.October, 1, "pchum_ben", "nager", model.ConfidenceComputed),
		mk(2027, time.October, 2, "pchum_ben", "nager", model.ConfidenceComputed),
	}); err != nil {
		t.Fatal(err)
	}

	replacement := []model.Holiday{
		mk(2027, time.October, 2, "pchum_ben", "nager", model.ConfidenceComputed),
	}
	removed, inserted, err := st.ReplaceYear(ctx, 2027, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 || inserted != 1 {
		t.Fatalf("removed/inserted = %d/%d, want 2/1", removed, inserted)
	}

	got, err := st.List(ctx, Filter{Year: 2027})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Date.Day() != 2 {
		t.Fatalf("replacement result = %+v, want only October 2", got)
	}
}

func TestOpenSupportsReservedCharactersInPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "holidays ? #.db")
	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.Years(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSupportsRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("data", 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := Open(context.Background(), "data/holidays.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := os.Stat("data/holidays.db"); err != nil {
		t.Fatalf("relative database file was not created in data/: %v", err)
	}
}
