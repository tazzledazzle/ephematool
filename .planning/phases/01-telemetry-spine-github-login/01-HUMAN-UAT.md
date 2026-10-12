---
status: partial
phase: 01-telemetry-spine-github-login
source: [01-VERIFICATION.md]
started: 2026-10-12T06:39:37Z
updated: 2026-10-12T06:39:37Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Real GitHub login with a real org
expected: With a GitHub OAuth App (scope read:org, callback http://localhost:8080/api/auth/github/callback, approved by an org owner if the org restricts third-party apps) and GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET, ALLOWED_ORGS, ADMIN_LOGINS set in .env, after `bazel run //:down && bazel run //:up`: (a) "Continue with GitHub" signs in an org member, who lands on the status page with all 10 services Healthy and the correct Admin or Viewer badge; (b) closing and reopening the browser keeps the session; (c) Sign out lands on /login; (d) a GitHub user outside the allowed orgs sees the Access denied page naming the required org.
result: [pending]

### 2. Open in Grafana after a real login
expected: "Open in Grafana" opens Grafana; a page-load trace can be followed from the dashboard through the API to the worker, and to its correlated logs in Loki.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
