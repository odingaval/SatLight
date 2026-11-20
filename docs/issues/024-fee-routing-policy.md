# Fee/routing-cost policy inconsistency (product decision)

Labels: documentation

Priority: Medium (code, docs, Terms, and UI disagree).

## Problem

The ledger spec says routing fees come out of platform fee revenue; the
withdrawal policy says the maintainer bears routing; the Terms say fee payer,
minimum, and routing treatment are TBD. The API computes 2% floor-1-sat, which
only matches one reading. This must be one consistent rule everywhere.

## Evidence

- [ledger-and-accounting.md](/home/bocal/SatLight/docs/specs/ledger-and-accounting.md:30)
  vs [012-withdrawal-policy.md](/home/bocal/SatLight/docs/012-withdrawal-policy.md:32)
  vs [terms-of-service.md](/home/bocal/SatLight/docs/legal/terms-of-service.md:27).
- [M2_COMPLETION_GUIDE.md](/home/bocal/SatLight/docs/guides/M2_COMPLETION_GUIDE.md:93)
  flags the same gap.

## Acceptance criteria

- [ ] Single product+counsel decision recorded (ADR-010 finalized).
- [ ] Code, OpenAPI/UI display, ledger entries, and Terms all match; tests lock it.
