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
- [ ] Unknown invoices are ignored; underpayments are not credited; overpayments follow the documented policy; expired invoices cannot credit funds.
- [ ] The app handles LND being unavailable and clearly reports that it cannot create an invoice.

### 2. Wallet and ledger stay in agreement

- [ ] Deposit, bounty reservation, cancellation, expiry, and any supported withdrawal update wallet and ledger in the same database transaction.
- [ ] Every ledger transaction balances; entries cannot be edited or deleted; cached available/reserved balances cannot become negative.
- [ ] Reconciliation detects a mismatch between ledger-derived balances and wallet caches, records the result, and alerts or pauses money movement according to the operations policy.
- [ ] A withdrawal endpoint either performs and tracks a real, idempotent withdrawal or is clearly unavailable. It must never report “queued” when no withdrawal was persisted or queued.

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
- A periodic reconciliation job runs hourly and at worker startup, compares wallet caches to ledger-derived balances, checks ledger transactions balance, and records its report in the audit log.
- Bounty fees use the documented 2% basis-point calculation rounded down with a 1-sat minimum when a nonzero fee rate is configured.
- Bounty listing applies cursor conditions before ordering and limiting, uses creation time plus bounty ID for stable pages, and rejects malformed cursors with HTTP 400.
- Unit tests cover authentication, ledger entry validation, GitHub helpers, cancellation and expiry eligibility, fee rounding, and bounty cursor construction. `go test ./...` passed on 2026-10-23.

## Known gaps to close

- Simultaneous requests using the same deposit idempotency key are now serialized with a transaction-scoped PostgreSQL advisory lock. A process/database failure after LND creates an invoice but before its deposit row commits can still leave an untracked invoice; safe recovery needs invoice metadata/reconciliation or an explicit pending-request protocol. There is no expiry worker for deposits, so expired status and late settlement handling need to be completed.
- Reconciliation still needs an agreed definition of platform liabilities, a comparison with LND node balance, and an operational alert or money-movement pause on mismatch.
- Project withdrawal is explicitly unavailable: the authenticated endpoint returns HTTP 501 and does not imply that funds were queued. Product and Terms still need to align on whether and when project owners may withdraw available funds.
- The bounty cursor query has unit coverage for SQL clause order and parameter values. Database-backed coverage for filter/cursor combinations is still needed.
- State transitions are not yet routed through the single transition function described by the spec; transitions other than cancellation and expiry are not yet audited.
- The implemented fee math has unit coverage, but fee and routing-cost policy still needs agreement across product docs, UI, and Terms.
- Terms and Privacy are still drafts with unresolved placeholders, and the legal/regulatory review and M1 audit remain outstanding.
- The current tests do not exercise database transactions, concurrent funding/reservations, or LND regtest behavior.

## What M2 does not complete

M2 does not send contributor payouts or decide whether a pull request qualifies. M3 verifies GitHub installations, pull requests, and merges. M4 implements reliable Lightning payouts, retries, claim links, monitoring, and beta operations. M2 must leave escrowed rewards accurately reserved so those later milestones can use them safely.

## Related specifications

- [Roadmap](ROADMAP.md)
- [Bounty state machine](../specs/state-machine.md)
- [Ledger and accounting](../specs/ledger-and-accounting.md)
- [Lightning integration](../specs/lightning-integration.md)
- [Testing strategy](../specs/testing-strategy.md)
- [Terms of Service draft](../legal/terms-of-service.md)
- [Privacy Policy draft](../legal/privacy-policy.md)
- [Regulatory memo template](../legal/regulatory-memo.md)
