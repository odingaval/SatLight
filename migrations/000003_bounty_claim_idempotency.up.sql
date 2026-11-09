BEGIN;

-- Idempotent bounty creation: retries reuse the same (project, key) row.
-- The hash detects key reuse with different request data (HTTP 409).
ALTER TABLE bounties
  ADD COLUMN idempotency_key TEXT,
  ADD COLUMN idempotency_hash TEXT;
CREATE UNIQUE INDEX bounties_project_idempotency_idx
  ON bounties(project_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

-- Idempotent claims: retries reuse the same (bounty, user, key) row.
ALTER TABLE claims
  ADD COLUMN idempotency_key TEXT;
CREATE UNIQUE INDEX claims_bounty_user_idempotency_idx
  ON claims(bounty_id, user_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

-- Disputes remember the pre-dispute state for mutual resolutions.
ALTER TABLE disputes
  ADD COLUMN previous_state TEXT;

COMMIT;
