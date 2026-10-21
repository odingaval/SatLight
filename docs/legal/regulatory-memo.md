# Regulatory Analysis Memo (template for counsel)

> Prepared for review by a qualified lawyer. Not legal advice. Complete before M2.

## 1. Facts to give counsel
- Satlight receives Bitcoin from maintainers over Lightning, holds it in a platform-controlled node and ledger, and pays contributors in Bitcoin on a verified GitHub event.
- 2% fee retained. No fiat conversion. No KYC in v1.
- Users may be in any country, including Kenya, Argentina, Brazil, Japan, the US, and the EU.
- Launch is a closed beta with five projects.

## 2. Questions
1. **Money transmission / VASP status:** Does custodial escrow make Satlight a money transmitter (US FinCEN, state licenses), a virtual asset service provider (FATF, EU MiCA, Japan, Brazil, Argentina, Kenya VASP Act), or similar? Does any exemption (payment processor, agent of payee, escrow) apply?
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

## 4. Risk register
| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Unlicensed money transmission | Unknown | Severe | Counsel opinion before M2; non-custodial pivot option |
| Sanctions exposure | Medium | High | Screening, geoblocking |
| Tax reporting gaps | Medium | Medium | Export tools, counsel guidance |
