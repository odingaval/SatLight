# Monitoring gaps: metrics, alerts, dashboards

Labels: ops,feature

Priority: Medium (flying blind on money-critical signals).

## Problem

Only five Prometheus series exist (payout outcomes/duration, drift, recon runs,
webhooks). The monitoring spec also requires payout failure rate, payout queue
depth/age, node sync/peers, outbound liquidity, API error rate/latency, plus
alert rules, Grafana dashboards, and runbook-linked on-call routing.

## Evidence

- [metrics.go](/home/bocal/SatLight/internal/metrics/metrics.go:13) vs
  [monitoring.md](/home/bocal/SatLight/docs/specs/monitoring.md:3) metric table.

## Acceptance criteria

- [ ] Missing metrics added with the documented thresholds (failure rate,
      p95 latency, queue age, liquidity floor, drift, 5xx rate).
- [ ] Alert rules + Grafana dashboards (Payouts, Liquidity, Webhooks,
      Reconciliation, API) committed; alerts link runbooks and page on-call.
