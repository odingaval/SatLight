BEGIN;
DROP TABLE IF EXISTS system_flags;
DROP TABLE IF EXISTS reconciliation_runs;
DROP TABLE IF EXISTS unmatched_receipts;
ALTER TABLE deposits
  DROP COLUMN IF EXISTS node_cancelled_at,
  DROP COLUMN IF EXISTS node_expired_at,
  DROP COLUMN IF EXISTS settled_late,
  DROP COLUMN IF EXISTS received_sats;
COMMIT;
