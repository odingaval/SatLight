package queue

import (
	"context"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Client struct {
	*river.Client[pgxpool.Tx]
}

func NewClient(pool *pgxpool.Pool) *Client {
	client, err := riverpgxv5.NewClient(pool, &river.Config{})
	if err != nil {
		panic(err)
	}
	return &Client{Client: client}
}

func JobRegistry() *river.JobRegistry {
	registry := river.NewJobRegistry()
	registry.MustRegister(&ProcessPayoutJob{})
	registry.MustRegister(&ProcessWebhookJob{})
	registry.MustRegister(&ReconciliationJob{})
	registry.MustRegister(&ClaimExpiryJob{})
	return registry
}

type ProcessPayoutArgs struct {
	PayoutID int64 `json:"payout_id"`
}

type ProcessPayoutJob struct {
	river.Job[ProcessPayoutArgs]
}

func (ProcessPayoutJob) Kind() string { return "process_payout" }

func (j *ProcessPayoutJob) Run(ctx context.Context, worker river.JobWorker[ProcessPayoutArgs]) error {
	return nil
}

type ProcessWebhookArgs struct {
	DeliveryID string `json:"delivery_id"`
}

type ProcessWebhookJob struct {
	river.Job[ProcessWebhookArgs]
}

func (ProcessWebhookJob) Kind() string { return "process_webhook" }

func (j *ProcessWebhookJob) Run(ctx context.Context, worker river.JobWorker[ProcessWebhookArgs]) error {
	return nil
}

type ReconciliationArgs struct{}

type ReconciliationJob struct {
	river.Job[ReconciliationArgs]
}

func (ReconciliationJob) Kind() string { return "reconciliation" }

func (j *ReconciliationJob) Run(ctx context.Context, worker river.JobWorker[ReconciliationArgs]) error {
	return nil
}

type ClaimExpiryArgs struct {
	ClaimID int64 `json:"claim_id"`
}

type ClaimExpiryJob struct {
	river.Job[ClaimExpiryArgs]
}

func (ClaimExpiryJob) Kind() string { return "claim_expiry" }

func (j *ClaimExpiryJob) Run(ctx context.Context, worker river.JobWorker[ClaimExpiryArgs]) error {
	return nil
}