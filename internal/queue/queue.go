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

// NewClient returns an insert-only River client. It registers every job kind
// (required by River even for inserts) with service-less workers; jobs are
// executed by the worker binary, never by this client, so Start must never be
// called on it.
func NewClient(pool *pgxpool.Pool) *Client {
	driver := riverpgxv5.New(pool)
	workers := river.NewWorkers()
	river.AddWorker(workers, &ProcessPayoutJob{})
	river.AddWorker(workers, &ProcessWebhookJob{})
	river.AddWorker(workers, &SendWebhookJob{})
	river.AddWorker(workers, &ReconciliationJob{})
	river.AddWorker(workers, &ClaimExpiryJob{})
	river.AddWorker(workers, &BountyExpiryJob{})
	client, err := river.NewClient(driver, &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
			"payouts":          {MaxWorkers: 5},
			"webhooks":         {MaxWorkers: 10},
			"reconciliation":   {MaxWorkers: 1},
		},
		Workers: workers,
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

func (c *Client) InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts ...river.InsertOpts) error {
	var insertOpts *river.InsertOpts
	if len(opts) > 0 {
		insertOpts = &opts[0]
	}
	_, err := c.Client.InsertTx(ctx, tx, args, insertOpts)
	return err
}

type ProcessPayoutArgs struct {
	BountyID int64 `json:"bounty_id"`
}

func (ProcessPayoutArgs) Kind() string { return "process_payout" }

type ProcessPayoutJob struct {
	river.WorkerDefaults[ProcessPayoutArgs]
	Service PayoutProcessor
}

type PayoutProcessor interface {
	ProcessBountyPayout(ctx context.Context, bountyID int64) error
}

func (j *ProcessPayoutJob) Work(ctx context.Context, job *river.Job[ProcessPayoutArgs]) error {
	return j.Service.ProcessBountyPayout(ctx, job.Args.BountyID)
}

type SendWebhookArgs struct {
	DeliveryID int64 `json:"delivery_id"`
}

func (SendWebhookArgs) Kind() string { return "send_webhook" }

type SendWebhookJob struct {
	river.WorkerDefaults[SendWebhookArgs]
	Service WebhookSender
}

type WebhookSender interface {
	SendWebhookDelivery(ctx context.Context, deliveryID int64) error
}

func (j *SendWebhookJob) Work(ctx context.Context, job *river.Job[SendWebhookArgs]) error {
	return j.Service.SendWebhookDelivery(ctx, job.Args.DeliveryID)
}

type ProcessWebhookArgs struct {
	DeliveryID string `json:"delivery_id"`
}

func (ProcessWebhookArgs) Kind() string { return "process_webhook" }

type ProcessWebhookJob struct {
	river.WorkerDefaults[ProcessWebhookArgs]
	Service WebhookProcessor
}

type WebhookProcessor interface {
	ProcessWebhookDelivery(ctx context.Context, deliveryID string) error
}

func (j *ProcessWebhookJob) Work(ctx context.Context, job *river.Job[ProcessWebhookArgs]) error {
	return j.Service.ProcessWebhookDelivery(ctx, job.Args.DeliveryID)
}

type ReconciliationArgs struct{}

func (ReconciliationArgs) Kind() string { return "reconciliation" }

type ReconciliationJob struct {
	river.WorkerDefaults[ReconciliationArgs]
	Service ReconciliationRunner
}

type ReconciliationRunner interface {
	RunReconciliation(ctx context.Context) error
}

func (j *ReconciliationJob) Work(ctx context.Context, job *river.Job[ReconciliationArgs]) error {
	return j.Service.RunReconciliation(ctx)
}

type ClaimExpiryArgs struct {
	ClaimID int64 `json:"claim_id"`
}

func (ClaimExpiryArgs) Kind() string { return "claim_expiry" }

type ClaimExpiryJob struct {
	river.WorkerDefaults[ClaimExpiryArgs]
	Service ClaimExpirer
}

type ClaimExpirer interface {
	ExpireClaim(ctx context.Context, claimID int64) error
}

func (j *ClaimExpiryJob) Work(ctx context.Context, job *river.Job[ClaimExpiryArgs]) error {
	return j.Service.ExpireClaim(ctx, job.Args.ClaimID)
}

type BountyExpiryArgs struct {
	BountyID int64 `json:"bounty_id"`
}

func (BountyExpiryArgs) Kind() string { return "bounty_expiry" }

type BountyExpirer interface {
	ExpireBounty(ctx context.Context, bountyID int64) error
}

type BountyExpiryJob struct {
	river.WorkerDefaults[BountyExpiryArgs]
	Service BountyExpirer
}

func (j *BountyExpiryJob) Work(ctx context.Context, job *river.Job[BountyExpiryArgs]) error {
	return j.Service.ExpireBounty(ctx, job.Args.BountyID)
}
