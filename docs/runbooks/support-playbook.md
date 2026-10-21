# Support Playbook

## Channels and targets
Email support@<your-domain> and a GitHub Discussions category. First response within 1 business day; payout issues within 4 hours.

## Common cases
| Issue | Steps |
|---|---|
| Payout failed | Check payout status and last error; ask contributor to verify Lightning address and wallet online; retry; if still failing issue claim link |
| Payout missing but merged | Verify PR merged, author = claim holder, claim active; check hold/review queue; check payouts paused |
| Wrong Lightning address | If not yet sent, update (cooldown applies); if sent, funds are irreversible: explain |
| Claim expired | Explain expiry; re-claim if bounty open |
| Maintainer will not merge | Open dispute; follow dispute process |
| Deposit not credited | Look up payment hash; confirm invoice settled; manual credit only via ledger with a second approver |
| Account locked/banned | Review flags; provide reason; appeal path |
| Refund request | Apply refund policy; only unreserved or cancelled funds |

## Rules
- Never ask for seed phrases, private keys, or full macaroons.
- Verify identity through GitHub login before discussing account details.
- Log every manual action in `audit_log` with a reason.
- Escalate suspected fraud or funds at risk to the incident lead.

## Macros
Prepare templated replies for the cases above, each following: what happened, what it means for funds, next step.
