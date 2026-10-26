# M2 Completion Guide: Funding and Escrow

M2 is complete when a maintainer can fund a project wallet, publish a bounty using those funds, and cancel or expire an open bounty with every sat accounted for. The system must remain correct when requests are repeated, concurrent, interrupted, or rejected.

M2 establishes the money and bounty accounting that later milestones rely on. GitHub merge verification belongs to M3; automatic contributor payouts belong to M4. M2 still needs to preserve the reserved funds and record the state needed for those later steps.

## The basic money flow

1. A maintainer asks Satlight to fund a project wallet for a chosen amount.
2. Satlight asks LND for a Lightning invoice and stores its payment hash, amount, project, and expiry.
3. When LND reports settlement, Satlight credits the project's **available** balance and records the matching ledger transaction atomically.
4. When a bounty is published, Satlight checks and locks the project wallet, moves the reward from **available** to **reserved**, records the ledger transaction, and opens the bounty in one database transaction.
5. If an open bounty is cancelled or expires, Satlight returns the reserved reward to the project's available balance and records the refund. If the bounty proceeds to a verified merge, later milestone code pays the contributor.

Example: a 100,000 sat deposit creates 100,000 sats available. Publishing a 20,000 sat bounty leaves 80,000 available and 20,000 reserved. Cancelling that open bounty returns the wallet to 100,000 available and zero reserved. A wallet balance alone is not proof of correctness: the ledger must explain every movement.

## Terms to understand

| Term | Meaning |
|---|---|
| Invoice | A Lightning request for a specific payment. It has a payment hash and an expiry. |
| Available balance | Funds a project can use for a new bounty or, if supported, withdraw. |
| Reserved balance / escrow | Funds set aside for active bounties and unavailable for another use. |
| Double-entry ledger | An append-only record where every transaction has equal total debits and credits. |
| Idempotency key | A client request identifier that makes a retry safe: the same request returns the original result instead of moving funds twice. |
| Reconciliation | A check that compares cached wallet balances and ledger balances, and ultimately the platform's liabilities with the Lightning node's funds. |
| Atomic transaction | A group of database changes that either all commit or all roll back. |

All amounts are integer satoshis. The wallet is a fast balance cache; the ledger is the accounting record. A wallet update without its ledger entries, or a ledger entry without the matching wallet update, is a failure.

## M2 completion checklist

### 1. Funding works end to end

- [ ] A project owner can request a fixed-amount invoice; another user cannot fund or inspect that project wallet.
- [ ] Invoice creation and deposit persistence handle retries safely. Reusing a key with different request data is rejected, and concurrent retries cannot create confusing duplicate invoices.
- [ ] A settled invoice credits the wallet and ledger once, even if LND replays the settlement update or the service reconnects after interruption.
- [ ] Unknown settled invoice receipts are quarantined for review; known settlements credit the actual amount received, including underpayments, overpayments, and settlements LND reports after app expiry.
- [ ] The app handles LND being unavailable and clearly reports that it cannot create an invoice.

### 2. Wallet and ledger stay in agreement

- [ ] Deposit, bounty reservation, cancellation, expiry, and any supported withdrawal update wallet and ledger in the same database transaction.
- [ ] Every ledger transaction balances; entries cannot be edited or deleted; cached available/reserved balances cannot become negative.
- [ ] Reconciliation compares node funds with liabilities and platform equity, accounts for in-flight payouts, persists results, and pauses funding/outflows on critical mismatches; paging and audited two-person resume remain to be completed.
- [x] Withdrawal endpoint is explicitly unavailable (HTTP 501); it does not claim to queue a request. Keep it blocked until the jurisdiction-specific custody/legal gate and destination-confirmation requirements are resolved.

### 3. Bounty funding and refunds are safe

- [ ] A bounty is validated before publishing: repository/issue, title, description, difficulty, reward, fee, and expiry follow documented rules.
- [ ] Publishing reserves exactly the reward in one transaction. Concurrent bounty requests cannot spend the same available balance; insufficient funds leave no bounty, ledger, or wallet changes behind.
- [ ] A repository issue cannot receive duplicate active bounties unless product policy explicitly allows it.
- [ ] Cancellation is allowed only in states that the policy permits. Open-bounty refunds are atomic and auditable; cancelling a claimed bounty cannot strand an active claim.
- [ ] Every supported state transition is validated and audited in the same transaction as its ledger effects.
- [ ] Expired open bounties are actually processed and refunded. The worker must do real work; a scheduled no-op is not expiry support.
- [ ] Search, filters, sorting, and pagination work together and have tests, including cursor queries.
- [ ] The platform fee calculation, minimum fee behavior, and routing-cost responsibility match the product decision, API/UI display, ledger entries, and Terms.

### 4. Policy and operational gates are resolved

- [ ] Terms and Privacy documents have real contact details and decisions in place of template brackets.
- [ ] Qualified counsel has reviewed the custodial escrow model, applicable regulatory obligations, fee/refund terms, and target jurisdictions, and documented a go/no-go and required restrictions.
- [ ] Node credentials use least-privilege macaroons and approved secret storage. Recovery, incident response, and a funding/payout kill switch have been exercised.
- [ ] The outstanding M1 audit is complete before real user funds are accepted. A passing unit test suite does not replace a security or accounting audit.

### 5. Verification demonstrates the invariants

- [ ] Unit tests cover fee calculations, state transitions, idempotency, and ledger invariants.
- [ ] Database integration tests cover transactions, concurrent reservations, rollback, unique constraints, and reconciliation.
- [ ] LND regtest covers invoice creation, settlement, replay, restart/reconnect, expiry, and node unavailability.
- [ ] The documented full test/build and migration checks pass in CI.

## What is already implemented

- Project funding requests create real LND invoices, persist deposit records, and reuse a prior invoice when the same project idempotency key is retried.
- Reusing a deposit idempotency key with a different amount now returns HTTP 409.
- A reconnecting invoice subscription settles known deposits and credits the project wallet and double-entry ledger atomically. Replayed settlements do not credit twice.
- Publishing a bounty reserves its reward and opens it in one database transaction; insufficient balance rolls back the operation.
- Open bounty cancellation refunds the wallet, writes ledger entries, and records an audit entry in one transaction. Claimed bounties are rejected for cancellation.
- Bounty publication schedules an expiry job in the same database transaction. The worker marks an expired open bounty as expired and refunds its reserved reward to the wallet and ledger atomically.
- Claiming a bounty schedules claim expiry in the same database transaction. An elapsed active claim is marked expired and reopens its bounty; if the bounty has also expired, the worker refunds the reservation and expires the bounty.
- A periodic reconciliation job runs hourly and at worker startup, compares wallet caches and aggregate project liabilities to ledger-derived balances and confirmed node funds, accounts for payouts marked `sending`, persists reports, and pauses funding/outflows on a detected shortfall or accounting mismatch. Money-movement flags start paused after migration.
- Bounty fees use the documented 2% basis-point calculation rounded down with a 1-sat minimum when a nonzero fee rate is configured.
- Bounty listing applies cursor conditions before ordering and limiting, uses creation time plus bounty ID for stable pages, and rejects malformed cursors with HTTP 400.
- Unit tests cover authentication, ledger entry validation, GitHub helpers, cancellation and expiry eligibility, fee rounding, and bounty cursor construction. `go test ./...` passes. The PostgreSQL integration target covers late/underpaid settlement idempotency, unmatched-receipt quarantine, concurrent bounty reservation, and fail-closed reconciliation pauses.

## Known gaps to close

- Withdrawal is a legal release gate, not an active product promise. The endpoint remains HTTP 501; Terms now say withdrawals are unavailable in preview and must be replaced after counsel chooses the permitted model. If Option A is approved, implement deposit aging, verified destination controls, idempotent persisted attempts, ledger accounting, and the outflow kill switch. If Option B is required, implement and audit the manual return process.
- ADR-011 and ADR-013 have been accepted and initial code is in place. Known deposit settlements record received amounts and late status; LND cancellation updates expire pending deposits; unmatched invoice receipts are quarantined and audit logged. Reconciliation now compares node balance to project liabilities and net platform fees, accounts for `sending` payouts, persists reports, and pauses new funding on serious mismatches or node-balance errors. Remaining: review tooling and paging for unmatched receipts, observing non-invoice/keysend HTLCs, paging/on-call integration, node reconnect-triggered reconciliation, an audited two-person resume path, and payout sender enforcement once payouts exist.
- The bounty cursor query has unit coverage for SQL clause order and parameter values. Database-backed coverage for filter/cursor combinations is still needed.
- State transitions are not yet routed through the single transition function described by the spec; transitions other than cancellation and expiry are not yet audited.
- The implemented fee math has unit coverage, but fee and routing-cost policy still needs agreement across product docs, UI, and Terms.
- Terms and Privacy now describe preview limitations and identify counsel decisions, but remain drafts with unresolved entity, jurisdiction, retention, contact, and regulatory answers. Legal/regulatory review and the M1 audit remain outstanding.
- Database integration coverage remains incomplete for rollback paths, deposit-creation crash recovery, bounty search/filter/cursor queries, and every reconciliation severity. LND regtest coverage, API authorization integration, and crash/reconnect scenarios remain outstanding.

## What M2 does not complete

M2 does not send contributor payouts or decide whether a pull request qualifies. M3 verifies GitHub installations, pull requests, and merges. M4 implements reliable Lightning payouts, retries, claim links, monitoring, and beta operations. M2 must leave escrowed rewards accurately reserved so those later milestones can use them safely.

## Related specifications

- [Roadmap](ROADMAP.md)
- [Bounty state machine](../specs/state-machine.md)
- [Ledger and accounting](../specs/ledger-and-accounting.md)
- [Lightning integration](../specs/lightning-integration.md)
- [Testing strategy](../specs/testing-strategy.md)
- [ADR-011: Deposit expiry and late payments](../011-late-deposits.md)
- [ADR-012: Project withdrawal policy](../012-withdrawal-policy.md)
- [ADR-013: Reconciliation mismatch response](../013-reconciliation-response.md)
- [Terms of Service draft](../legal/terms-of-service.md)
- [Privacy Policy draft](../legal/privacy-policy.md)
- [Regulatory memo template](../legal/regulatory-memo.md)
