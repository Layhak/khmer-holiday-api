package main

import "testing"

func TestTargetYearsParsesStrictRange(t *testing.T) {
	got, err := targetYears(0, "2024-2027")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0] != 2024 || got[3] != 2027 {
		t.Fatalf("targetYears = %v, want 2024 through 2027", got)
	}

	for _, raw := range []string{
		"2024-2027junk",
		"2024-2027-2028",
		"0-2027",
		"2027-2201",
		"2028-2027",
	} {
		if _, err := targetYears(0, raw); err == nil {
			t.Errorf("targetYears accepted invalid range %q", raw)
		}
	}
}
