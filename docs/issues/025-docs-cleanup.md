# Stale docs: ADR index, INDEX paths, limitations, spec notes

Labels: documentation

Priority: Low (misleads contributors; cheap to fix).

## Problem

Several docs contradict the implemented state: the ADR index still marks
accepted ADRs as Proposed; the docs index links to paths that do not exist; the
backend guide's "Known Limitations" still claims stub Lightning and no tests;
the main doc's ADR table says "to write"; the Lightning spec says ADR-011 is
unimplemented.

## Evidence

- [adr/README.md](/home/bocal/SatLight/docs/adr/README.md:3) vs Accepted 011/013.
- [INDEX.md](/home/bocal/SatLight/docs/guides/INDEX.md:5) (`../SKILLS.md`,
  `../openapi.yaml`, `../migrations/000001_init.up.sql` do not exist).
- [BACKEND_IMPLEMENTATION.md](/home/bocal/SatLight/docs/guides/BACKEND_IMPLEMENTATION.md:589)
  limitations vs real LND client + 10 test files.
- [lightning-integration.md](/home/bocal/SatLight/docs/specs/lightning-integration.md:19)
  ADR-011 "proposed and not implemented yet".

## Acceptance criteria

- [ ] ADR statuses match their decision files; INDEX links resolve.
- [ ] Limitations/spec notes rewritten to current state; dead "to write" notes gone.
