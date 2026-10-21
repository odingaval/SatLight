package queue

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type Client struct {
	*river.Client[pgx.Tx]
}

func NewClient(pool *pgxpool.Pool) *Client {
	driver := riverpgxv5.New(pool)
	client, err := river.NewClient(driver, &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
			"payouts":          {MaxWorkers: 5},
			"webhooks":         {MaxWorkers: 10},
			"reconciliation":   {MaxWorkers: 1},
		},
	})
	if err != nil {
		panic(err)
	}
	return &Client{Client: client}
}

func (c *Client) Insert(ctx context.Context, args river.JobArgs, opts ...river.InsertOpts) error {
	if len(opts) > 0 {
		_, err := c.Client.Insert(ctx, args, &opts[0])
		return err
	}
	_, err := c.Client.Insert(ctx, args, nil)
	return err
}

type ProcessPayoutArgs struct {
	PayoutID int64 `json:"payout_id"`
}

func (ProcessPayoutArgs) Kind() string { return "process_payout" }

type ProcessPayoutJob struct {
	river.WorkerDefaults[ProcessPayoutArgs]
}

func (j *ProcessPayoutJob) Work(ctx context.Context, job *river.Job[ProcessPayoutArgs]) error {
	return nil
}

type ProcessWebhookArgs struct {
	DeliveryID string `json:"delivery_id"`
}

func (ProcessWebhookArgs) Kind() string { return "process_webhook" }

type ProcessWebhookJob struct {
	river.WorkerDefaults[ProcessWebhookArgs]
}

func (j *ProcessWebhookJob) Work(ctx context.Context, job *river.Job[ProcessWebhookArgs]) error {
	return nil
}

type ReconciliationArgs struct{}

func (ReconciliationArgs) Kind() string { return "reconciliation" }

type ReconciliationJob struct {
	river.WorkerDefaults[ReconciliationArgs]
}

func (j *ReconciliationJob) Work(ctx context.Context, job *river.Job[ReconciliationArgs]) error {
	return nil
}

type ClaimExpiryArgs struct {
	ClaimID int64 `json:"claim_id"`
}

func (ClaimExpiryArgs) Kind() string { return "claim_expiry" }

type ClaimExpiryJob struct {
	river.WorkerDefaults[ClaimExpiryArgs]
}

func (j *ClaimExpiryJob) Work(ctx context.Context, job *river.Job[ClaimExpiryArgs]) error {
	return nil
}