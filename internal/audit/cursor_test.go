package audit

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 12, 20, 10, 36, 123456000, time.UTC) // microsecond precision, like Postgres
	id := [16]byte{0xf7, 0x7e, 0xcb, 0x51, 0xfe, 0x28, 0x41, 0xde, 0xbc, 0x31, 0xae, 0xd5, 0x44, 0x61, 0x3d, 0x8a}
	gotAt, gotID, err := decodeCursor(encodeCursor(at, id))
	if err != nil {
		t.Fatal(err)
	}
	if !gotAt.Equal(at) || gotID != id {
		t.Fatalf("round trip = %v %x, want %v %x", gotAt, gotID, at, id)
	}
}

func TestCursorRejectsTampering(t *testing.T) {
	good := encodeCursor(time.Now(), [16]byte{1})
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, c := range map[string]string{
		"not base64":   "!!!",
		"no separator": enc("2026-10-12T00:00:00Z"),
		"bad time":     enc("yesterday|00000000-0000-0000-0000-000000000001"),
		"bad uuid":     enc("2026-10-12T00:00:00Z|nope"),
		"short uuid":   enc("2026-10-12T00:00:00Z|0001"),
		"truncated":    good[:len(good)-4],
	} {
		if _, _, err := decodeCursor(c); !errors.Is(err, ErrBadCursor) {
			t.Errorf("%s: err = %v, want ErrBadCursor", name, err)
		}
	}
}

func TestNormalizeLimit(t *testing.T) {
	for in, want := range map[int]int{0: 50, -3: 50, 1: 1, 100: 100, 500: 100} {
		if got := normalizeLimit(in); got != want {
			t.Errorf("normalizeLimit(%d) = %d, want %d", in, got, want)
		}
	}
}
