# No CI workflow; `make openapi` points at the wrong path

Labels: ops,testing

Priority: Medium (gates exist on paper only).

## Problem

There is no CI workflow, so the required gates (lint, vet, unit + integration
tests, govulncheck, secrets scan, OpenAPI validation, migration dry run) never
run automatically. Separately, `make openapi` validates `openapi.yaml` at the
repo root, but the spec lives at `docs/specs/openapi.yaml`.

## Evidence

- No `.github/workflows/` in the repo.
- [testing-strategy.md](/home/bocal/SatLight/docs/specs/testing-strategy.md:24)
  CI gates section.
- [Makefile](/home/bocal/SatLight/Makefile:62) wrong spec path.

## Acceptance criteria

- [ ] CI runs all documented gates on PRs; Lightning regtest nightly + on
      payout-code changes.
- [ ] `make openapi` validates `docs/specs/openapi.yaml` with a pinned tool.
- [ ] Secrets scan and migration dry run included; failures block merge.
