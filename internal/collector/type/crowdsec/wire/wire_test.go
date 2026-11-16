package wire

import (
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
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

// Empty decision counts (metrics carrying none) and no counts (no metrics) must
// arrive as sent: the checks treat them differently.
func TestHeldKeepsEmptyApartFromNone(t *testing.T) {
	for _, held := range []map[string]int64{nil, {}, {"crowdsec": 3}} {
		raw, err := cbor.Marshal(Component{Kind: KindBouncer, Name: "fw", Held: held})
		if err != nil {
			t.Fatal(err)
		}
		var got Component
		if err := cbor.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if (got.Held == nil) != (held == nil) || len(got.Held) != len(held) {
			t.Errorf("held %#v arrived as %#v", held, got.Held)
		}
	}
}
