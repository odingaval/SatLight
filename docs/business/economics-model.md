# Economics Model

All numbers below are **illustrative formulas and example inputs**, not forecasts. Replace with real data.

## Revenue
`Fee revenue = completed bounty volume (sats) x 2%`

Example: 1,000,000 sats of bounties completed in a month yields 20,000 sats of fees (matches the proposal).

## Costs (monthly, inputs to fill in)
| Cost | Driver | Notes |
|---|---|---|
| Routing fees | payouts x average routing fee | Capped per payout by max-fee limit |
| Channel opens/closes | on-chain fees | Depends on fee environment |
| Infrastructure | hosting, DB, monitoring, email | Fixed + usage |
| Security/legal | audit, counsel, insurance | One-off + recurring |
| Team | designer, developer, copywriter | Largest cost |
| Liquidity capital | sats locked in channels | Opportunity cost |

## Break-even
`Break-even volume = monthly costs / 0.02` (in sats-equivalent value).
Example: if monthly costs equal [X] USD, required monthly bounty volume is X / 0.02 = 50 x X USD.

## Key sensitivities
- Average bounty size: smaller bounties raise per-payout costs relative to the fee. For a 500-sat reward the fee is 10 sats; routing must stay below that. Consider a minimum fee (e.g. 50 sats) or a minimum reward.
- Payout fee limits vs. network fees.
- Liquidity required: roughly the outbound payouts between top-ups.
- BTC price changes affect fiat-denominated costs but not sat-denominated revenue.

## Unit economics worksheet
| Input | Value |
|---|---|
| Average reward (sats) | [ ] |
| Platform fee (2%) | = reward x 0.02 |
| Avg routing fee (sats) | [ ] |
| Net per payout | fee - routing |
| Payouts per month | [ ] |
| Monthly fixed costs | [ ] |
| Break-even payouts | fixed / net per payout |

## Decision to make
Is a pure 2% fee viable at very small rewards? Options: minimum fee, higher fee on small bounties, premium maintainer features (analytics, dashboards, reputation), or sponsored funding pools.
