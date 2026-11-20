# GitHub polish: suggest-claim comment, ETag caching, backoff

Labels: feature,tech-debt

Priority: Medium (contributor UX + API quota).

## Problem

PRs from contributors without a claim are silently ignored instead of getting
the spec's "suggest claiming" comment, and API reads have no ETag caching or
`403/429` backoff, risking quota exhaustion at scale.

## Evidence

- [github-integration.md](/home/bocal/SatLight/docs/specs/github-integration.md:30)
  edge table vs `processPullRequestEvent`
  ([service.go](/home/bocal/SatLight/internal/github/service.go:216)).
- Rate-limit section (:38) unimplemented.

## Acceptance criteria

- [ ] Non-claimant PRs get a helpful comment (rate-limited, deduped per PR).
- [ ] Conditional requests + `Retry-After` backoff on GitHub reads.
- [ ] Tests with recorded fixtures for both paths.
