package wire

import (
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	want := time.Date(2026, 11, 15, 3, 44, 29, 123456789, time.UTC)
	for _, v := range []string{
		"2026-11-15T03:44:29.123456789Z",
		"2026-11-15T10:44:29.123456789+07:00",
		"2026-11-15 03:44:29.123456789 +0000 UTC",
		"2026-11-15 10:44:29.123456789 +0700 +07",
		"2026-11-15 03:44:29.123456789 +0000 UTC m=+0.100000001",
	} {
		got, ok := ParseTime(v)
		if !ok || !got.Equal(want) {
			t.Errorf("ParseTime(%q) = %v, %v", v, got, ok)
		}
	}
	if got, ok := ParseTime("2026-11-15T03:44:29Z"); !ok || !got.Equal(want.Truncate(time.Second)) {
		t.Errorf("whole seconds: %v %v", got, ok)
	}
	for _, v := range []string{"", "yesterday", "0001-01-01 00:00:00 +0000 UTC", "0001-01-01T00:00:00Z"} {
		if _, ok := ParseTime(v); ok {
			t.Errorf("ParseTime(%q) must fail", v)
		}
	}
}
