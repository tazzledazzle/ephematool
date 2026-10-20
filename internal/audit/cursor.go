package audit

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBadCursor is returned for a cursor that is not one this package issued.
var ErrBadCursor = errors.New("audit: invalid cursor")

const (
	defaultLimit = 50
	maxLimit     = 100
)

// normalizeLimit applies the default (50) and the cap (100).
func normalizeLimit(n int) int {
	switch {
	case n <= 0:
		return defaultLimit
	case n > maxLimit:
		return maxLimit
	}
	return n
}

// uuidString formats 16 bytes as a canonical UUID.
func uuidString(b [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// parseUUID is the inverse of uuidString.
func parseUUID(s string) ([16]byte, error) {
	var out [16]byte
	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(raw) != 16 || len(s) != 36 {
		return out, ErrBadCursor
	}
	copy(out[:], raw)
	return out, nil
}

// encodeCursor makes the opaque cursor "<occurred_at RFC3339Nano>|<uuid>"
// (base64url). It is opaque to clients, not secret: it only names a position.
func encodeCursor(at time.Time, id [16]byte) string {
	raw := at.UTC().Format(time.RFC3339Nano) + "|" + uuidString(id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a cursor, returning ErrBadCursor for anything else.
func decodeCursor(c string) (time.Time, [16]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, [16]byte{}, ErrBadCursor
	}
	tsPart, idPart, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, [16]byte{}, ErrBadCursor
	}
	at, err := time.Parse(time.RFC3339Nano, tsPart)
	if err != nil {
		return time.Time{}, [16]byte{}, ErrBadCursor
	}
	id, err := parseUUID(idPart)
	if err != nil {
		return time.Time{}, [16]byte{}, ErrBadCursor
	}
	return at, id, nil
}
