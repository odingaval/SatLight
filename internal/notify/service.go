package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/queue"
)

// maxWebhookAttempts bounds maintainer-webhook retries before a delivery is
// marked failed. Spacing comes from River's job retry schedule.
const maxWebhookAttempts = 8

type Service struct {
	cfg  *config.Config
	pool *pgxpool.Pool
	log  zerolog.Logger
	http *http.Client
	// sendMail is smtp.SendMail without auth (local relay), injectable for tests.
	sendMail func(addr, from string, to []string, msg []byte) error
}

func NewService(cfg *config.Config, pool *pgxpool.Pool) *Service {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger()
	return &Service{
		cfg:  cfg,
		pool: pool,
		log:  logger,
		http: &http.Client{Timeout: 10 * time.Second},
		sendMail: func(addr, from string, to []string, msg []byte) error {
			return smtp.SendMail(addr, nil, from, to, msg)
		},
	}
}

func parseSMTPAddr(raw string) (string, error) {
	addr := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw), "smtp://"), "smtps://")
	addr = strings.TrimSuffix(addr, "/")
	if addr == "" || !strings.Contains(addr, ":") {
		return "", fmt.Errorf("invalid SMTP_URL %q: want smtp://host:port", raw)
	}
	return addr, nil
}

// Notify stores and sends one email. It is idempotent on (kind, refType,
// refID): an already-sent notification is skipped. Failures are recorded and
// returned; callers must never fail money operations on notification errors.
func (s *Service) Notify(ctx context.Context, kind string, userID int64, refType string, refID int64, subject, body string) error {
	var email *string
	if err := s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
		return err
	}
	if email == nil || *email == "" {
		s.log.Warn().Str("kind", kind).Int64("user_id", userID).Msg("user has no email; skipping notification")
		return nil
	}
	res, err := s.pool.Exec(ctx, `
		INSERT INTO notifications (kind, user_id, email, subject, body, ref_type, ref_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (kind, ref_type, ref_id) DO NOTHING
	`, kind, userID, *email, subject, body, refType, refID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		var sent bool
		if err := s.pool.QueryRow(ctx, `SELECT sent_at IS NOT NULL FROM notifications WHERE kind = $1 AND ref_type = $2 AND ref_id = $3`,
			kind, refType, refID).Scan(&sent); err != nil || sent {
			return err
		}
	}
	addr, err := parseSMTPAddr(s.cfg.SMTPURL)
	if err != nil {
		return err
	}
	msg := "From: " + s.cfg.EmailFrom + "\r\n" +
		"To: " + *email + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	if err := s.sendMail(addr, s.cfg.EmailFrom, []string{*email}, []byte(msg)); err != nil {
		_, _ = s.pool.Exec(ctx, `UPDATE notifications SET last_error = $1 WHERE kind = $2 AND ref_type = $3 AND ref_id = $4`,
			err.Error(), kind, refType, refID)
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE notifications SET sent_at = now(), last_error = NULL WHERE kind = $1 AND ref_type = $2 AND ref_id = $3`,
		kind, refType, refID)
	return err
}

func (s *Service) NotifyPayoutSettled(ctx context.Context, userID, bountyID, amountSats int64) error {
	return s.Notify(ctx, "payout_settled", userID, "bounty", bountyID,
		fmt.Sprintf("You received %d sats", amountSats),
		fmt.Sprintf("Your bounty #%d was paid.\n\nAmount: %d sats (fee already deducted).\n\nView your payout history at %s.\n",
			bountyID, amountSats, s.cfg.WebURL))
}

func (s *Service) NotifyClaimLink(ctx context.Context, userID, payoutID int64, token string, amountSats int64) error {
	link := strings.TrimRight(s.cfg.BaseURL, "/") + "/api/v1/payouts/claim/" + token
	return s.Notify(ctx, "payout_claim_link", userID, "payout", payoutID,
		fmt.Sprintf("Claim your %d sats", amountSats),
		fmt.Sprintf("Automatic payment failed, but your %d sats are reserved.\n\nOpen this link in any Lightning wallet to pull your payment:\n%s\n\nThis link is single-use. Keep it private.\n",
			amountSats, link))
}

func (s *Service) NotifyMergeReverted(ctx context.Context, ownerID, bountyID int64) error {
	return s.Notify(ctx, "merge_reverted", ownerID, "bounty", bountyID,
		fmt.Sprintf("Payout held for bounty #%d", bountyID),
		fmt.Sprintf("The merge behind bounty #%d could not be re-verified, so the automatic payout was held.\n\nNo funds moved. Review the pull request and resolve the bounty from your dashboard.\n",
			bountyID))
}

func (s *Service) NotifyDestinationChanged(ctx context.Context, userID int64) error {
	return s.Notify(ctx, "payout_destination_changed", userID, "user", userID,
		"Your payout destination changed",
		"Your Lightning payout destination was just changed.\n\nIf this was you, no action is needed. New destinations cannot receive automatic payouts for 48 hours.\n\nIf this was not you, contact support immediately.\n")
}

// EnqueueProjectEvent records a maintainer webhook delivery and enqueues it,
// inside the caller's transaction. Projects without a configured URL are
// skipped silently.
func (s *Service) EnqueueProjectEvent(ctx context.Context, tx pgx.Tx, q *queue.Client, projectID int64, event string, payload map[string]any) error {
	var target *string
	if err := tx.QueryRow(ctx, `SELECT notification_webhook_url FROM projects WHERE id = $1`, projectID).Scan(&target); err != nil {
		return err
	}
	if target == nil || *target == "" {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var deliveryID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO webhook_deliveries (project_id, url, event, payload)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, projectID, *target, event, string(data)).Scan(&deliveryID); err != nil {
		return err
	}
	return q.InsertTx(ctx, tx, &queue.SendWebhookArgs{DeliveryID: deliveryID}, river.InsertOpts{Queue: "webhooks"})
}

// SendWebhookDelivery POSTs one queued maintainer-webhook delivery. Failures
// return an error for River retry until the attempt budget is spent.
func (s *Service) SendWebhookDelivery(ctx context.Context, deliveryID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var url, event string
	var payload []byte
	var status string
	var attempts int
	var secret *string
	err = tx.QueryRow(ctx, `
		SELECT d.url, d.event, d.payload, d.status, d.attempts, p.notification_webhook_secret
		FROM webhook_deliveries d LEFT JOIN projects p ON d.project_id = p.id
		WHERE d.id = $1 FOR UPDATE
	`, deliveryID).Scan(&url, &event, &payload, &status, &attempts, &secret)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if status == "sent" {
		return tx.Commit(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Satlight-Event", event)
	if secret != nil && *secret != "" {
		mac := hmac.New(sha256.New, []byte(*secret))
		mac.Write(payload)
		req.Header.Set("X-Satlight-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := s.http.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err = errors.New("webhook returned status " + resp.Status)
		}
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET status = 'sent', last_error = NULL WHERE id = $1`, deliveryID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if attempts+1 >= maxWebhookAttempts {
		if _, dbErr := tx.Exec(ctx, `UPDATE webhook_deliveries SET status = 'failed', attempts = attempts + 1, last_error = $1 WHERE id = $2`, err.Error(), deliveryID); dbErr != nil {
			return dbErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return commitErr
		}
		s.log.Error().Err(err).Int64("delivery_id", deliveryID).Msg("maintainer webhook delivery failed permanently")
		return nil
	}
	if _, dbErr := tx.Exec(ctx, `UPDATE webhook_deliveries SET attempts = attempts + 1, last_error = $1 WHERE id = $2`, err.Error(), deliveryID); dbErr != nil {
		return dbErr
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return err
}
