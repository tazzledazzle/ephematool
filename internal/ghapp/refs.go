package ghapp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

func splitRepo(repo string) (owner, name string) {
	owner, name, _ = strings.Cut(repo, "/")
	return owner, name
}

// ResolveRef turns a branch, PR number or (short) sha into a full 40-char
// commit sha. Input is validated first, so invalid refs never reach GitHub.
func (c *Client) ResolveRef(ctx context.Context, repo string, t RefType, ref string) (string, error) {
	if err := ValidateRefInput(t, ref); err != nil {
		return "", err
	}
	owner, name := splitRepo(repo)
	switch t {
	case RefBranch:
		r, resp, err := c.gh.Git.GetRef(ctx, owner, name, "heads/"+ref)
		if statusOf(resp, err) == http.StatusNotFound {
			return "", ErrRefNotFound
		}
		if err != nil {
			return "", err
		}
		return r.GetObject().GetSHA(), nil

	case RefPR:
		n, _ := strconv.Atoi(ref)
		pr, resp, err := c.gh.PullRequests.Get(ctx, owner, name, n)
		if statusOf(resp, err) == http.StatusNotFound {
			return "", ErrRefNotFound
		}
		if err != nil {
			return "", err
		}
		// Only open PRs from the same repository: a fork's code must never be
		// built and run on the platform (fork head.repo differs; a deleted fork
		// has a nil head.repo).
		head := pr.GetHead()
		if pr.GetState() != "open" || head.GetRepo() == nil ||
			!strings.EqualFold(head.GetRepo().GetFullName(), repo) {
			return "", ErrPRNotEligible
		}
		return head.GetSHA(), nil

	default: // RefSHA: confirm the commit exists in this repo and expand it
		cm, resp, err := c.gh.Repositories.GetCommit(ctx, owner, name, ref, nil)
		if statusOf(resp, err) == http.StatusNotFound {
			return "", ErrRefNotFound
		}
		if err != nil {
			return "", err
		}
		return cm.GetSHA(), nil
	}
}
