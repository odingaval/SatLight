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
1. Maintainer requests amount; API creates an invoice (1 hour expiry, project-stamped memo) and stores `deposits` row.
2. Invoice subscription marks the deposit `settled`; an hourly poller looks every pending deposit up at the node and credits settlements the stream missed (node-cancelled invoices expire their rows). A widowed-invoice sweep re-attaches settled invoices that lost their deposit rows.
3. In one DB transaction: set deposit settled, write ledger entries (`node:liquidity` debit, `project:{id}:available` credit), update wallet.
4. Overpayment is credited. Underpayment is normally prevented by fixed-amount BOLT11 invoices, but settlement handling must use the actual amount received. Per accepted [ADR-011](../011-late-deposits.md), the node determines expiry, known late settlements are credited at the actual received amount, and unknown receipts are quarantined for review.

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

## Macaroons (least privilege)
The node connection is split in two; each side carries only the macaroon it needs:

| Connection | Macaroon | Calls |
|---|---|---|
| Receive (`lightningClient`) | default `invoice.macaroon` (`invoices:read+write`) | AddInvoice, ListInvoices, LookupInvoice, SubscribeInvoices |
| Send (`routerClient` + `paymentClient`) | baked `payment.macaroon` (`offchain:read+write`, `onchain:read`) | SendPaymentV2, TrackPaymentV2, DecodePayReq, WalletBalance, ChannelBalance |

Decode and balance reads live on the send side deliberately: they need `offchain`/`onchain` reads the invoice macaroon must never have. Bake the payment macaroon with:

```bash
lncli bakemacaroon offchain:read offchain:write onchain:read \
  --save_to=/data/chain/bitcoin/regtest/payment.macaroon
```

Never use the admin macaroon for app traffic: it can spend, close channels, and reconfigure the node. The regtest suite (`make test-ln`) is the one exception — throwaway test nodes where baking would add nothing.

## Rules
- **Persist before pay.** The attempt and payment hash are committed before sending.
- **Unknown outcome is not failure.** On timeout or crash, call `LookupPayment` by hash; only retry if the previous attempt is definitively failed.
- **Bound the send client-side.** `PayInvoice` enforces a 90s deadline on top of LND's `TimeoutSeconds`, which v0.21.4 does not honor when the only peer disconnects. Timeouts return `ErrPaymentTimedOut`; the payout layer then checks the hash (settle if succeeded, hold if in-flight, fail only on definitive `FAILED`) so attempts and claim links are never spent on a payment that may still settle.
- **One payout per bounty** is enforced by a unique constraint.
- **Fee limit:** max(10 sats, 1% of amount) by default (`PAYOUT_MAX_FEE_*`).
- **Retry schedule:** 1m, 5m, 30m, 2h, 12h. Then issue an LNURL-withdraw claim link (`payouts.claim_link_token`) and email the contributor. Claim links expire after 30 days; unclaimed funds return to the project wallet after notice.
- **Kill switch:** `PAYOUTS_PAUSED=true` queues payouts without sending.

## Liquidity
- Alert when outbound capacity < 2x the largest trailing-24h payout total.
- Keep at least two channels with well-connected peers; document rebalancing in `docs/runbooks/operations.md`.

## Testing
Use regtest with two LND nodes (Polar). Required cases: success, no route, recipient LNURL server down, invoice expired, crash after send before DB commit, duplicate job, node restart mid-payment.
