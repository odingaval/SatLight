# Monitoring and Observability

## Metrics
| Metric | Alert |
|---|---|
| Payout success rate (first try) | below 95% over 15 min |
| Payout latency (merge to settled) | p95 above 60 s |
| Payout queue depth / oldest job age | oldest above 10 min |
| Webhook failures / signature rejects | spike above baseline |
| Node sync and peer connectivity | any offline |
| Outbound liquidity | below 2x daily payout total |
| Reconciliation drift | any mismatch (critical, pauses payouts) |
| API error rate and latency | 5xx above 1% |
| DB replication / disk | thresholds |

## Logging
Structured JSON logs with request IDs and bounty/payout IDs. Redact secrets, tokens, macaroons, and full invoices.

## Tracing and dashboards
Trace API to queue to node calls. Grafana dashboards: Payouts, Liquidity, Webhooks, Reconciliation, API.

## On-call
Pager for critical alerts; documented runbooks linked from each alert.
