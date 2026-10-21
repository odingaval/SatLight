# Lightning Integration Spec

## Interface
```go
type LightningClient interface {
    CreateInvoice(ctx, amountSats int64, memo string, expiry time.Duration) (Invoice, error)
    SubscribeInvoices(ctx) (<-chan InvoiceUpdate, error)
    ResolveLightningAddress(ctx, addr string, amountSats int64) (bolt11 string, err error)
    PayInvoice(ctx, bolt11 string, maxFeeSats int64, timeout time.Duration) (PaymentResult, error)
    LookupPayment(ctx, paymentHash []byte) (PaymentStatus, error)
    Balance(ctx) (NodeBalance, error)
}
```

## Deposits
1. Maintainer requests amount; API creates an invoice (1 hour expiry) and stores `deposits` row.
2. Invoice subscription (plus a periodic poll as backup) marks the deposit `settled`.
3. In one DB transaction: set deposit settled, write ledger entries (`node:liquidity` debit, `project:{id}:available` credit), update wallet.
4. Underpayment is impossible for BOLT11 amounts; overpayment is credited; expired invoices are ignored.

## Payouts (sequence)
```
merge verified -> bounty merged -> enqueue payout job
worker: lock bounty row; verify state=merged and no payout exists
        resolve destination -> bolt11 for (reward - fee)
        TX: insert payout(pending) + payout_attempt(payment_hash); commit
        PayInvoice(max fee limit)
        success: TX: payout settled, bounty paid, ledger entries (payout, fee, routing cost)
        failure: record error; schedule retry
```

## Rules
- **Persist before pay.** The attempt and payment hash are committed before sending.
- **Unknown outcome is not failure.** On timeout or crash, call `LookupPayment` by hash; only retry if the previous attempt is definitively failed.
- **One payout per bounty** is enforced by a unique constraint.
- **Fee limit:** max(10 sats, 1% of amount) by default (`PAYOUT_MAX_FEE_*`).
- **Retry schedule:** 1m, 5m, 30m, 2h, 12h. Then issue an LNURL-withdraw claim link (`payouts.claim_link_token`) and email the contributor. Claim links expire after 30 days; unclaimed funds return to the project wallet after notice.
- **Kill switch:** `PAYOUTS_PAUSED=true` queues payouts without sending.

## Liquidity
- Alert when outbound capacity < 2x the largest trailing-24h payout total.
- Keep at least two channels with well-connected peers; document rebalancing in `docs/runbooks/operations.md`.

## Testing
Use regtest with two LND nodes (Polar). Required cases: success, no route, recipient LNURL server down, invoice expired, crash after send before DB commit, duplicate job, node restart mid-payment.
