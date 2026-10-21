package ghapp

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRefInput(t *testing.T) {
	ok := []struct {
		t   RefType
		ref string
	}{
		{RefBranch, "feature/x"}, {RefBranch, "release/1.2"}, {RefBranch, "main"},
		{RefPR, "12"}, {RefPR, "999999999"},
		{RefSHA, "abc1234"}, {RefSHA, strings.Repeat("a", 40)},
	}
	for _, c := range ok {
		if err := ValidateRefInput(c.t, c.ref); err != nil {
			t.Errorf("%s %q: unexpected %v", c.t, c.ref, err)
		}
	}
	bad := []struct {
		t   RefType
		ref string
	}{
		{RefBranch, "../x"}, {RefBranch, "-x"}, {RefBranch, "/x"}, {RefBranch, "a b"},
		{RefBranch, "%2e"}, {RefBranch, strings.Repeat("a", 201)}, {RefBranch, "x;rm"}, {RefBranch, ""},
		{RefBranch, "a..b"},
		{RefPR, "12a"}, {RefPR, "0123456789"}, {RefPR, ""},
		{RefSHA, "XYZ1234"}, {RefSHA, "abc12"}, {RefSHA, strings.Repeat("a", 41)},
		{RefType("tag"), "v1"},
	}
	for _, c := range bad {
		if err := ValidateRefInput(c.t, c.ref); !errors.Is(err, ErrRefInvalid) {
			t.Errorf("%s %q: err = %v, want ErrRefInvalid", c.t, c.ref, err)
		}
	}
}

func TestImageTag(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	for format, want := range map[string]string{
		"{sha}": sha, "{sha7}": "0123456", "sha-{sha7}": "sha-0123456", "v{sha}": "v" + sha,
	} {
		if got := ImageTag(format, sha); got != want {
			t.Errorf("ImageTag(%q) = %q, want %q", format, got, want)
		}
	}
}
