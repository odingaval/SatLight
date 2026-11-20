# Legal docs sign-off, SECURITY placeholders, M1 audit

Labels: legal,security

Priority: Blocker (no real funds before counsel + audit).

## Problem

Terms and Privacy are drafts full of `[COUNSEL DECISION]` placeholders; the
regulatory memo (including Kenya VASP Act 2025 licensing analysis) awaits a
jurisdiction-specific go/no-go; `SECURITY.md` still has `security@<your-domain>`
and a placeholder PGP fingerprint; and the M1 security/accounting audit is
outstanding while M2 holds escrow design.

## Evidence

- [terms-of-service.md](/home/bocal/SatLight/docs/legal/terms-of-service.md:3),
  [privacy-policy.md](/home/bocal/SatLight/docs/legal/privacy-policy.md:3),
  [regulatory-memo.md](/home/bocal/SatLight/docs/legal/regulatory-memo.md:3).
- [SECURITY.md](/home/bocal/SatLight/docs/guides/SECURITY.md:6) placeholders.
- [ROADMAP.md](/home/bocal/SatLight/docs/guides/ROADMAP.md:5) "audit outstanding".

## Acceptance criteria

- [ ] Counsel completes Terms/Privacy/regulatory review with launch jurisdictions,
      licensing, KYC/AML, retention, and entity decisions recorded.
- [ ] Real security contact + PGP published; private reporting path tested.
- [ ] M1 audit (security + accounting) completed; findings tracked as issues.
