package db

import "time"

// Timestamps are stored as INTEGER unix epoch seconds — UTC by definition,
// so they compare and ORDER BY chronologically with no format to drift.
// The owning tables are STRICT (migration 007): binding a raw time.Time —
// which the driver would store as TEXT — is rejected by the type system, so
// timestamps must always be bound through ToUnix/ToUnixPtr.
func ToUnix(t time.Time) int64 {
	return t.Unix()
}

func ToUnixPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.Unix()
	return &v
}

// FromUnix is the scan-side counterpart: epoch seconds to UTC time.Time.
func FromUnix(v int64) time.Time {
	return time.Unix(v, 0).UTC()
}
