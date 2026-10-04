package db

import "time"

// ToUnix converts t to epoch seconds. STRICT tables (migration 007) reject a
// raw time.Time, so always bind timestamps through ToUnix or ToUnixPtr.
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

// FromUnix converts epoch seconds to a UTC time.Time.
func FromUnix(v int64) time.Time {
	return time.Unix(v, 0).UTC()
}
