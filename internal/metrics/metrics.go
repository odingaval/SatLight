// Package metrics exposes Prometheus metrics for money-critical operations.
//
// Alert thresholds (see docs/specs/monitoring.md): payout failure rate above
// 2% over 15 minutes, reconciliation drift beyond ±100 sats, any critical
// reconciliation run, or webhook processing errors.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// PayoutsCompleted counts terminal payout outcomes: settled,
	// claimed_via_link, claim_link_issued, or held.
	PayoutsCompleted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "satlight_payouts_completed_total",
		Help: "Terminal payout outcomes.",
	}, []string{"outcome"})

	// PayoutDuration tracks time spent processing one payout job.
	PayoutDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "satlight_payout_duration_seconds",
		Help:    "Time spent processing one payout job.",
		Buckets: []float64{1, 5, 15, 30, 60, 120, 300},
	})

	// ReconciliationDrift is the latest node-minus-books drift in sats.
	ReconciliationDrift = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "satlight_reconciliation_drift_sats",
		Help: "Latest node-minus-books drift in sats.",
	})

	// ReconciliationRuns counts reconciliation runs by severity.
	ReconciliationRuns = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "satlight_reconciliation_runs_total",
		Help: "Reconciliation runs by severity.",
	}, []string{"severity"})

	// WebhooksReceived counts recorded GitHub webhook deliveries by event.
	WebhooksReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "satlight_webhooks_received_total",
		Help: "Recorded GitHub webhook deliveries by event.",
	}, []string{"event"})
)
