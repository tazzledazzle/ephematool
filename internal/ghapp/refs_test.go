package ghapp

import (
	"errors"
	"strings"
	"testing"

	"github.com/ephematool/ephematool/internal/config"
)

const demoRepo = "ephematool-e2e/demo-app"

func TestResolveRef(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	mainSHA, err := f.c.ResolveRef(ctx, demoRepo, RefBranch, "main")
	if err != nil || len(mainSHA) != 40 {
		t.Fatalf("main: %q %v", mainSHA, err)
	}
	featSHA, err := f.c.ResolveRef(ctx, demoRepo, RefBranch, "feature/x")
	if err != nil || featSHA == mainSHA {
		t.Fatalf("feature/x: %q %v", featSHA, err)
	}
	if _, err := f.c.ResolveRef(ctx, demoRepo, RefBranch, "missing-x"); !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("missing-x: %v, want ErrRefNotFound", err)
	}
	if got, err := f.c.ResolveRef(ctx, demoRepo, RefPR, "1"); err != nil || got != featSHA {
		t.Fatalf("PR 1: %q %v, want the feature/x sha", got, err)
	}
	for _, pr := range []string{"2", "3", "4"} { // fork, deleted fork, closed
		if _, err := f.c.ResolveRef(ctx, demoRepo, RefPR, pr); !errors.Is(err, ErrPRNotEligible) {
			t.Errorf("PR %s: %v, want ErrPRNotEligible", pr, err)
		}
	}
	if got, err := f.c.ResolveRef(ctx, demoRepo, RefSHA, mainSHA[:7]); err != nil || got != mainSHA {
		t.Fatalf("short sha: %q %v, want %q", got, err, mainSHA)
	}
}

func TestResolveRefInvalidInputMakesNoRequests(t *testing.T) {
	f := newFixture(t)
	before := f.rec.requests
	for _, c := range []struct {
		t   RefType
		ref string
	}{{RefBranch, "../x"}, {RefBranch, "x;rm"}, {RefPR, "12a"}, {RefSHA, "XYZ1234"}} {
		if _, err := f.c.ResolveRef(t.Context(), demoRepo, c.t, c.ref); !errors.Is(err, ErrRefInvalid) {
			t.Errorf("%s %q: %v", c.t, c.ref, err)
		}
	}
	if f.rec.requests != before {
		t.Fatalf("invalid input caused %d HTTP requests, want 0", f.rec.requests-before)
	}
}

func TestImageRef(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	sha, _ := f.c.ResolveRef(ctx, demoRepo, RefBranch, "main")

	got, err := f.c.ImageRef(ctx, demoRepo, sha)
	if err != nil || !strings.HasPrefix(got, "ghcr.io/ephematool-e2e/demo-app@sha256:") || len(got) != len("ghcr.io/ephematool-e2e/demo-app@sha256:")+64 {
		t.Fatalf("ImageRef = %q %v", got, err)
	}
	noimg, _ := f.c.ResolveRef(ctx, demoRepo, RefBranch, "noimage-x")
	if _, err := f.c.ImageRef(ctx, demoRepo, noimg); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("noimage: %v, want ErrImageNotFound", err)
	}
	if _, err := f.c.ImageRef(ctx, "somebody/else", sha); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("unknown repo (403): %v, want ErrImageNotFound", err)
	}
	// Mixed-case repo input still resolves: GHCR names are lowercase.
	if _, err := f.c.ImageRef(ctx, "Ephematool-E2E/Demo-App", sha); err != nil {
		t.Fatalf("mixed case repo: %v", err)
	}
}

func TestImageRefSha7Format(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.ImageTagFormat = "{sha7}" })
	sha, _ := f.c.ResolveRef(t.Context(), demoRepo, RefBranch, "main")
	if _, err := f.c.ImageRef(t.Context(), demoRepo, sha); err != nil {
		t.Fatalf("sha7 tag: %v", err)
	}
	if f.rec.count("HEAD /v2/ephematool-e2e/demo-app/manifests/"+sha[:7]) == 0 {
		t.Fatal("the registry was not asked for the 7-char tag")
	}
}
