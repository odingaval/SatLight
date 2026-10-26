BEGIN;

ALTER TABLE deposits
  ADD COLUMN received_sats BIGINT CHECK (received_sats IS NULL OR received_sats > 0),
  ADD COLUMN settled_late BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN node_expired_at TIMESTAMPTZ,
  ADD COLUMN node_cancelled_at TIMESTAMPTZ;

CREATE TABLE unmatched_receipts (
  id BIGSERIAL PRIMARY KEY,
  payment_hash BYTEA NOT NULL UNIQUE,
  amount_sats BIGINT NOT NULL CHECK (amount_sats > 0),
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','credited','held')),
  reviewed_by TEXT,
  reviewed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE reconciliation_runs (
  id BIGSERIAL PRIMARY KEY,
  checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  node_balance_sats BIGINT,
  liabilities_sats BIGINT,
  equity_sats BIGINT,
  in_flight_sats BIGINT NOT NULL DEFAULT 0,
  drift_sats BIGINT,
  severity TEXT NOT NULL CHECK (severity IN ('ok','info','warning','critical','error')),
  report JSONB NOT NULL
);

CREATE TABLE system_flags (
  name TEXT PRIMARY KEY,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  reason TEXT,
  changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  changed_by TEXT NOT NULL DEFAULT 'system'
);

-- Money movement remains explicitly paused until counsel and operations approve launch.
INSERT INTO system_flags (name, enabled, reason)
VALUES ('funding_paused', TRUE, 'launch legal and operational review required'),
       ('outflows_paused', TRUE, 'launch legal and operational review required');

COMMIT;
