# No QR rendering for funding invoices

Labels: feature

Priority: Medium (spec-required funding UX).

## Problem

The fund-wallet card shows the BOLT11 string with a copy button but no QR code,
although QR rendering is a required frontend skill and the design system calls
for a QR invoice panel. Mobile wallet funding is painful.

## Evidence

- [pages.ts](/home/bocal/SatLight/web/src/pages.ts:118) renders text + copy only.
- [SKILLS.md](/home/bocal/SatLight/docs/guides/SKILLS.md:24) requires QR rendering.

## Acceptance criteria

- [ ] Funding card renders a scannable QR with copyable text fallback and screen-
      reader label per the accessibility checklist.
- [ ] Invoice expiry surfaced with a "create new invoice" action.
