-- Claim links expire (default 30 days) so unclaimed funds can return to the
-- project wallet. Deliberately no BEGIN/COMMIT: ALTER TYPE ... ADD VALUE
-- cannot run inside a transaction block, and each statement here is
-- independently safe to apply.
ALTER TYPE payout_status ADD VALUE IF NOT EXISTS 'expired';

ALTER TABLE payouts ADD COLUMN IF NOT EXISTS claim_link_expires_at TIMESTAMPTZ;

-- Outstanding links expire 30 days after issue, per the Lightning spec.
UPDATE payouts
SET claim_link_expires_at = created_at + interval '30 days'
WHERE status = 'claim_link_issued' AND claim_link_expires_at IS NULL;
