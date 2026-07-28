package model

import (
	"bytes"
	"encoding/json"
	"time"
)

// marshalWithDates encodes v, then splices in the derived date fields that the
// API contract promises but that the struct stores as time.Time.
func marshalWithDates(v any, date, updated time.Time) ([]byte, error) {
	base, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	extra := map[string]any{
		"date":    date.Format(DateLayout),
		"year":    date.Year(),
		"month":   int(date.Month()),
		"day":     date.Day(),
		"weekday": date.Weekday().String(),
	}
	if !updated.IsZero() {
		extra["updated_at"] = updated.UTC().Format(time.RFC3339)
	}

	add, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}

	// Splice the two objects: `{"a":1}` + `{"b":2}` -> `{"a":1,"b":2}`.
	if bytes.Equal(base, []byte("{}")) {
		return add, nil
	}
	out := make([]byte, 0, len(base)+len(add))
	out = append(out, base[:len(base)-1]...)
	out = append(out, ',')
	out = append(out, add[1:]...)
	return out, nil
}
