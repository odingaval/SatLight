BEGIN;

-- Two-person resume ledger: a paused flag clears only after one operator
-- proposes and a second operator approves. At most one open proposal per flag.
CREATE TABLE flag_resumptions (
  id          BIGSERIAL PRIMARY KEY,
  flag_name   TEXT NOT NULL CHECK (flag_name IN ('funding_paused','outflows_paused')),
  proposed_by TEXT NOT NULL,
  reason      TEXT NOT NULL,
  proposed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  approved_by TEXT,
  approved_at TIMESTAMPTZ,
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','approved','rejected'))
);
CREATE UNIQUE INDEX flag_resumptions_one_open_per_flag ON flag_resumptions(flag_name) WHERE status = 'open';

COMMIT;
