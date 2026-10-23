# ADR-011: Deposit expiry and late payments

- **Status:** Proposed
- **Date:** 2026-10-23

## Context
Deposits are Lightning invoices with a 1 hour expiry. Lightning has no return address, so Satlight cannot automatically send money back to a payer. Most nodes reject new payments once an invoice has expired, so true late payments should be rare, but they can happen: a payment already in flight at expiry, clock differences between our app and the node, or a payment that reaches the node with no invoice we recognize (for example a keysend). The earlier spec said expired invoices are "ignored". That is unsafe: if sats actually arrive and we ignore them, the node holds funds the ledger does not know about, reconciliation reports a mismatch, and a maintainer has paid without being credited.

## Decision
1. **The node decides expiry.** The app marks a deposit `expired` only after the node reports the invoice expired or canceled. The app clock alone never rejects money.
2. **Money received is money credited.** If a payment settles against a known `payment_hash`, credit the project wallet even if it arrives after `expires_at`. Record `settled_late = true` and the actual amount received (`received_sats`), which is what is credited, whether it is more or less than requested.
3. **Unknown receipts are quarantined, not guessed.** A payment with no matching deposit goes to `unmatched_receipts` with status `open`. It is not credited automatically. Staff review within 2 business days: credit to the project if the sender can be tied to it, otherwise hold. Do not auto-refund (no return path); refund only against an invoice the payer provides.
4. **Crediting is idempotent** on `payment_hash` (unique constraint), so a repeated node event cannot credit twice.
5. **Alerts:** a late credit is logged and shown to the maintainer; more than 5 late credits in 24h, or any unmatched receipt, alerts the on-call.
6. **UI and Terms** tell maintainers not to pay expired invoices and explain that any payment that does arrive is credited.

## Consequences
- The ledger always explains the node balance, so reconciliation stays meaningful.
- Maintainers are never silently charged for a payment that is not credited.
- Adds the `unmatched_receipts` table and a small manual review process.
- Replaces the "expired invoices are ignored" rule in the Lightning integration spec.
