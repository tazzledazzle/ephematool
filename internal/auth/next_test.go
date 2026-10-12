package auth

import "testing"

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"/":                   "/",
		"/status?x=1":         "/status?x=1",
		"//evil":              "/",
		"https://evil":        "/",
		"/\\evil":             "/",
		"":                    "/",
		"javascript:alert(1)": "/",
		"/a\nb":               "/",
		"status":              "/",
	}
	for in, want := range cases {
		if got := SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
