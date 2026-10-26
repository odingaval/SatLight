# Satlight Terms of Service (DRAFT)

> **Internal draft for qualified counsel. Not legal advice. Do not publish or use to accept funds.** Items marked `[COUNSEL DECISION]` remain release blockers.

Last updated: 2026-10-26

**Release status:** The public frontend is a preview. Satlight has not received legal clearance to custody or transfer user Bitcoin. Do not accept real funds or promise a live withdrawal service until counsel has confirmed the operating jurisdictions, required licences, and launch conditions. The withdrawal endpoint is not available.

## 1. Acceptance and eligibility
By using Satlight you agree to these Terms. You must be at least 18 (or the age of majority where you live) and not barred from using the service under applicable law or sanctions. If you use Satlight for an organization, you confirm you may bind it.

## 2. The service
Satlight is developing a marketplace where maintainers may fund bounties in Bitcoin and contributors may receive payment over the Lightning Network after a linked GitHub pull request is merged. The service is not currently open for real-money use. Any future custody, escrow, deposit, withdrawal, or payout service is subject to regulatory approval and revised Terms. [COUNSEL DECISION: confirm the legal characterization and permitted service scope.]

## 3. Accounts
You sign in with GitHub. You are responsible for securing your account and providing accurate information. If money movement is enabled in a future release, separate terms will describe identity checks, destination verification, and payout eligibility. [COUNSEL DECISION: age/eligibility, sanctions, identity and verification requirements.]

## 4. Funding and escrow
- Deposits and money movement are not enabled for public use in this preview.
- If approved for launch, maintainers will receive a Lightning invoice before a deposit is requested. A payment that settles to a known invoice will be credited using the amount reported by the Lightning node. A payment to an expired invoice may still settle and will be credited if the node reports settlement. Unknown receipts may be held for manual review.
- Funds reserved for an active bounty will remain unavailable until that bounty is paid, cancelled, or expires under the then-current refund policy.
- **Withdrawals are not currently available.** Before any real deposits are accepted, counsel and the product owner must choose and publish either a controlled withdrawal policy for unreserved funds or a reviewed manual-return policy. The current code does not process withdrawals.
- No interest is paid on held funds, if a future approved service permits custody.
- [COUNSEL DECISION: custody disclosure, segregation/insolvency treatment, withdrawal/refund rights, deposit finality and applicable licensing.]

## 5. Fees
The proposed product fee is 2% of each completed bounty reward. The fee payer, minimum fee, routing-cost treatment, tax treatment, and disclosure language must be confirmed before launch. The API currently calculates a 2% fee with a one-sat minimum when fees are enabled. [COUNSEL AND PRODUCT DECISION.]

## 6. Bounties, claims, and payouts
- A bounty has a description, difficulty, reward, repository link, and expiry.
- A contributor may claim a bounty for [7] days. A claim expires if no qualifying pull request is linked.
- A payout is triggered when Satlight verifies that a linked pull request by the claim holder has been merged into the repository.
- Payouts are sent over Lightning to the contributor's destination. If delivery fails, Satlight retries and may issue a claim link. Payments are irreversible once settled.
- Satlight does not guarantee that maintainers will merge any contribution.

## 7. Refunds, expiry, and cancellation
For the development preview, no public funds should be accepted. A future release must publish the Dispute and Refund Policy before accepting funds. The current product proposal returns the reserved reward for an open bounty to the project wallet when it is cancelled or expires; merged work is not refundable. [COUNSEL DECISION: abandoned projects, claims/disputes, refunds, and how users recover unreserved funds if the service closes.]

## 8. Disputes
Either party may open a dispute. Satlight reviews evidence and may decide to pay, cancel, or propose another resolution. Satlight's decision on fund release is final within the platform, without limiting rights under law.

## 9. Prohibited conduct
Fraud, collusion or self-dealing to obtain payouts, spam submissions, abuse of the API, circumventing limits, illegal activity, harassment, uploading malicious code, or using Satlight to evade sanctions. Violations may lead to forfeited payouts, freezing, or termination.

## 10. Intellectual property
Contributions remain subject to the repository's license and contributor terms. Satlight claims no ownership of contributed code. Maintainers are responsible for ensuring they may accept and license contributions. Satlight's name, logo, and software (under [AGPL-3.0]) are governed by their respective terms.

## 11. Bitcoin and technology risks
Bitcoin and Lightning payments are irreversible. Network congestion, routing failures, software bugs, or third-party outages may delay payments. Bitcoin's value is volatile. Satlight does not provide financial, tax, or legal advice. You are responsible for taxes on amounts you receive or pay.

## 12. Disclaimers and limitation of liability
The service is provided "as is" and "as available" to the extent permitted by applicable law. [COUNSEL DECISION: permitted disclaimers, liability cap, mandatory consumer rights, and jurisdiction-specific wording.]

## 13. Indemnity
Any indemnity applies only to the extent permitted by applicable law. [COUNSEL DECISION: scope, consumer limitations, and enforceability.]

## 14. Suspension and termination
We may suspend or terminate access for security, abuse, or legal reasons. As the current preview does not accept funds, it has no live withdrawal process. Any future fund-return process, including legal or fraud holds, must be described in launch Terms. [COUNSEL DECISION.]

## 15. Governing law and disputes
[COUNSEL DECISION: identify the contracting entity, governing law, courts or arbitration, and consumer/collective-action rules after launch jurisdictions are selected.]

## 16. Changes
We may update these Terms with notice. Continued use means acceptance.

## 17. Contact
[COUNSEL DECISION: publish a monitored legal contact for the operating entity.]
