package db

import (
	"testing"
	"time"
)

func TestToUnix_RoundTrip(t *testing.T) {
	cet := time.FixedZone("CET", 3600)
	ts := time.Date(2025, 11, 17, 15, 8, 44, 970035330, cet)

	got := ToUnix(ts)
	want := time.Date(2025, 11, 17, 14, 8, 44, 0, time.UTC).Unix()
	if got != want {
		t.Errorf("ToUnix = %d, want %d", got, want)
	}
	if back := FromUnix(got); !back.Equal(ts.Truncate(time.Second)) {
		t.Errorf("FromUnix(ToUnix) = %v, want %v", back, ts.Truncate(time.Second))
	}
	if loc := FromUnix(got).Location(); loc != time.UTC {
		t.Errorf("FromUnix location = %v, want UTC", loc)
	}
}

func TestToUnixPtr(t *testing.T) {
	if ToUnixPtr(nil) != nil {
		t.Error("ToUnixPtr(nil) should be nil")
	}
	ts := time.Date(2026, 3, 6, 21, 28, 41, 0, time.UTC)
	if got := ToUnixPtr(&ts); got == nil || *got != ts.Unix() {
		t.Errorf("ToUnixPtr = %v, want %d", got, ts.Unix())
	}
}
