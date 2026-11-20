# Browse filters: sort ignored, language is a substring match

Labels: bug

Priority: Medium (misleading marketplace filters).

## Problem

The browse UI offers reward sorting and language filtering, but the API ignores
`sort` entirely and implements `language` as a substring match on the repo name,
so `language=Go` matches any repository containing "go" (e.g. "django").

## Evidence

- `buildListBountiesQuery` ([service.go](/home/bocal/SatLight/internal/bounty/service.go:382))
  has no sort handling and matches language via `repo_full_name ILIKE %...%`.
- UI sends both in [pages.ts](/home/bocal/SatLight/web/src/pages.ts:28).

## Acceptance criteria

- [ ] `sort` (newest, reward) honored or removed from UI and OpenAPI.
- [ ] Language backed by real repo-language data or replaced with an honest
      repo-name search; docs updated.
- [ ] DB-backed tests for filter/sort/cursor combinations.
