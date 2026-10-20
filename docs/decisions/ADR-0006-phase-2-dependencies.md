# ADR-0006: Phase 2 pinned tools and modules

Status: accepted. Decided by: <your name>, <date>.

I verified the checksums and digests in `deploy/kind/versions.env` against the publishers'
pages (kind release digests, get.helm.sh checksum files, registry index digests, the Traefik
chart index). I approve adding `golang-jwt/jwt/v5` and `santhosh-tekuri/jsonschema/v6` to go.mod.
Everything else in versions.env is a developer tool installed under `.tools/` by `tools/kind.sh`
(Phase 2c), never a Go dependency.
