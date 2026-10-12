package auth

import (
	"net/url"
	"strings"
)

// SafeNext returns raw only when it is a single-slash relative path, else "/".
// It rejects protocol-relative ("//host"), "/\" tricks, schemes, hosts and
// control characters (open redirect, T-01-19).
func SafeNext(raw string) string {
	if raw == "" || raw[0] != '/' || strings.HasPrefix(raw, "//") || strings.ContainsRune(raw, '\\') {
		return "/"
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "/"
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}
	return raw
}
