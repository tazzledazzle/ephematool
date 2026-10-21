package ghapp

import (
	"errors"
	"regexp"
	"strings"
)

// Sentinel errors. Callers (internal/envs) map them to audited rejections.
var (
	ErrNotConfigured    = errors.New("ghapp: GitHub App is not configured")
	ErrRefInvalid       = errors.New("ghapp: ref is not valid for its type")
	ErrRefNotFound      = errors.New("ghapp: ref not found")
	ErrPRNotEligible    = errors.New("ghapp: pull request is not eligible")
	ErrImageNotFound    = errors.New("ghapp: image not found")
	ErrDispatchRejected = errors.New("ghapp: workflow dispatch rejected")
)

// RefType says how a user-supplied ref is interpreted.
type RefType string

// The three ways to point at code (ENV-01).
const (
	RefBranch RefType = "branch"
	RefPR     RefType = "pr"
	RefSHA    RefType = "sha"
)

var (
	branchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	prRe     = regexp.MustCompile(`^[0-9]{1,9}$`)
	shaRe    = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// ValidateRefInput rejects a ref before any network call is made. It is the
// first defence against SSRF/path injection through the ref field: go-github
// builds request paths from these values, so they must be boring.
func ValidateRefInput(t RefType, ref string) error {
	switch t {
	case RefBranch:
		if !branchRe.MatchString(ref) || strings.Contains(ref, "..") ||
			strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "-") {
			return ErrRefInvalid
		}
	case RefPR:
		if !prRe.MatchString(ref) {
			return ErrRefInvalid
		}
	case RefSHA:
		if !shaRe.MatchString(ref) {
			return ErrRefInvalid
		}
	default:
		return ErrRefInvalid
	}
	return nil
}

// ImageTag renders IMAGE_TAG_FORMAT for a commit: {sha} is the full 40 chars,
// {sha7} the first seven (D-23).
func ImageTag(format, sha string) string {
	short := sha
	if len(short) > 7 {
		short = short[:7]
	}
	return strings.NewReplacer("{sha7}", short, "{sha}", sha).Replace(format)
}
