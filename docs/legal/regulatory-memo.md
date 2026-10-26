# Regulatory Analysis Memo (template for counsel)

> Prepared for review by qualified counsel. Not legal advice. Do not accept real funds or operate the custodial flow before counsel records a jurisdiction-specific go/no-go.

Last updated: 2026-10-26

## 0. Current legal research snapshot (verify with counsel)
- Kenya is not only a hypothetical jurisdiction if the founder, entity, or service operations are based there. The Virtual Asset Service Providers Act, 2025 commenced on 4 November 2025. Its First Schedule identifies custodial virtual-asset wallet services and virtual-asset payment processing among the regulated service categories. [Official Act](https://new.kenyalaw.org/akn/ke/act/2025/20/eng%402025-11-04/source)
- The Central Bank of Kenya and Capital Markets Authority published that licensing would commence after implementing regulations; the CMA now lists the **Virtual Asset Service Providers Regulations 2026** in its regulations library. [CBK/CMA notice](https://www.centralbank.go.ke/uploads/press_releases/665231223_Public%20Notice%20on%20the%20Virtual%20Assets%20Service%20Providers%20Act%202025.pdf) · [CMA regulations listing](https://www.cma.or.ke/regulatory-framework/)
- The application’s proposed architecture receives sats into a platform-controlled LND node, allocates user balances in an internal ledger, and may transfer sats to contributors or maintainers. Counsel must decide whether those functions fall within one or more licensed categories and what entity, licence, governance, AML/CFT, safeguarding, disclosure, and data-protection obligations apply. This memo does not determine that classification.
- The official notice and regulations listing do not resolve Satlight’s specific status, eligibility, or licence. Counsel must inspect the final Gazette instruments and confirm the present application process with the relevant regulator. If Kenya is a launch jurisdiction, treat regulatory clearance as a hard blocker, especially because the statutory transition for existing providers is approaching in November 2026; counsel must confirm how it applies to a new or not-yet-operating product.
- Kenya’s Data Protection Act, 2019 sets data-subject rights and retention principles. The draft privacy notice still needs a lawful-basis, retention, processor, hosting-region, and rights-response review. [Official Act](https://new.kenyalaw.org/akn/ke/act/2019/24/eng%402019-11-15/source)

## 1. Facts to give counsel
- Satlight receives Bitcoin from maintainers over Lightning, holds it in a platform-controlled node and ledger, and pays contributors in Bitcoin on a verified GitHub event.
- 2% fee retained. No fiat conversion. No KYC in v1.
- Intended user countries and the operating entity's country are not yet decided. Do not assume the product may solicit users globally.
- Launch is a closed beta with five projects.

## 2. Questions
1. **Money transmission / VASP status:** For each target jurisdiction, does the exact custody and transfer flow make Satlight a regulated service provider? In Kenya, specifically assess the VASP Act 2025 and final 2026 Regulations against the proposed LND custody, internal wallet ledger, bounty escrow, contributor payouts, and maintainer withdrawals. Does any exemption apply?
2. **Non-custodial alternatives:** Would hold invoices or direct maintainer-to-contributor payments with Satlight only verifying events avoid licensing? What UX trade-offs?
3. **KYC/AML and sanctions:** What thresholds and screening apply? Can v1 limit amounts to stay below them?
4. **Geoblocking:** Which jurisdictions should be excluded at launch?
5. **Tax:** Platform reporting duties (e.g. US 1099 thresholds), VAT/GST on the 2% fee, contributor and maintainer tax treatment (worker classification).
6. **Consumer protection and terms:** Required disclosures, refund rules, dispute handling, limitation of liability enforceability.
7. **Data protection:** GDPR/UK GDPR, CCPA, LGPD, Kenya DPA, APPI obligations; data transfers.
8. **Entity and IP:** Where to incorporate, liability protection, AGPL and trademark strategy, contributor license (DCO or CLA).
9. **Open source specifics:** Any issues paying for contributions that affect maintainers' obligations or employer IP assignment for contributors.

## 3. Decision outputs needed
| Decision | Options | Owner |
|---|---|---|
| Custody model | Custodial / non-custodial / hybrid | Founder + counsel |
| Launch jurisdictions | list | Counsel |
| KYC approach | none / threshold-based / full | Counsel |
| Entity | jurisdiction and type | Founder |
| Withdrawal model | controlled self-service, reviewed manual return, or no custodial funds | Founder + counsel |
| Kenya status | licence required, licence path/timing, or no Kenya launch | Counsel + founder |

## 4. Risk register
| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Unlicensed money transmission | Unknown | Severe | Counsel opinion before M2; non-custodial pivot option |
| Sanctions exposure | Medium | High | Screening, geoblocking |
| Tax reporting gaps | Medium | Medium | Export tools, counsel guidance |
