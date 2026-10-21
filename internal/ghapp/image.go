package ghapp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// manifestAccept lists the manifest media types GHCR may answer with.
const manifestAccept = "application/vnd.oci.image.index.v1+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.docker.distribution.manifest.v2+json"

// ImageRef checks that ghcr.io/<org>/<repo>:<tag> exists using an anonymous
// registry token (D-23: no GitHub credential is sent to the registry) and
// returns the digest-pinned reference "ghcr.io/<org>/<repo>@sha256:...".
// Deploying by digest means a re-pushed tag cannot change what runs.
func (c *Client) ImageRef(ctx context.Context, repo, sha string) (string, error) {
	repo = strings.ToLower(repo)
	tag := ImageTag(c.cfg.ImageTagFormat, sha)
	base := strings.TrimRight(c.cfg.GHCRBaseURL, "/")
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", base, repo, url.PathEscape(tag))

	do := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, manifestURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", manifestAccept)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return c.hc.Do(req)
	}

	resp, err := do("")
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		token, terr := c.registryToken(ctx, resp.Header.Get("WWW-Authenticate"), repo)
		if terr != nil {
			return "", terr
		}
		if resp, err = do(token); err != nil {
			return "", err
		}
		resp.Body.Close()
	}
	switch resp.StatusCode {
	case http.StatusOK:
		digest := resp.Header.Get("Docker-Content-Digest")
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
			return "", fmt.Errorf("ghapp: registry returned no usable digest for %s:%s", repo, tag)
		}
		// The reference always names ghcr.io: the registry URL may point at a
		// fake in dev/e2e, but the workflow only accepts ghcr.io images.
		return fmt.Sprintf("ghcr.io/%s@%s", repo, digest), nil
	case http.StatusNotFound, http.StatusForbidden:
		// 403 is what GHCR answers for a package that is private or absent.
		return "", ErrImageNotFound
	default:
		return "", fmt.Errorf("ghapp: registry answered %d for %s:%s", resp.StatusCode, repo, tag)
	}
}

// registryToken follows the Bearer challenge in WWW-Authenticate and fetches an
// anonymous pull token for repo.
func (c *Client) registryToken(ctx context.Context, challenge, repo string) (string, error) {
	realm := challengeParam(challenge, "realm")
	if realm == "" {
		return "", fmt.Errorf("ghapp: registry challenge has no realm")
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("scope", "repository:"+repo+":pull")
	if svc := challengeParam(challenge, "service"); svc != "" {
		q.Set("service", svc)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ghapp: registry token endpoint answered %d", resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil || out.Token == "" {
		return "", fmt.Errorf("ghapp: registry token response unusable")
	}
	return out.Token, nil
}

// challengeParam extracts key="value" from a Bearer challenge header.
func challengeParam(header, key string) string {
	i := strings.Index(header, key+`="`)
	if i < 0 {
		return ""
	}
	rest := header[i+len(key)+2:]
	if j := strings.Index(rest, `"`); j >= 0 {
		return rest[:j]
	}
	return ""
}
