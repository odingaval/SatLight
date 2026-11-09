BEGIN;

ALTER TABLE disputes DROP COLUMN IF EXISTS previous_state;

DROP INDEX IF EXISTS claims_bounty_user_idempotency_idx;
ALTER TABLE claims DROP COLUMN IF EXISTS idempotency_key;

DROP INDEX IF EXISTS bounties_project_idempotency_idx;
ALTER TABLE bounties
  DROP COLUMN IF EXISTS idempotency_hash,
  DROP COLUMN IF EXISTS idempotency_key;

COMMIT;
