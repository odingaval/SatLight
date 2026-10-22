-- Satlight initial schema (PostgreSQL 15+). All money in integer sats.
BEGIN;

CREATE TYPE bounty_state AS ENUM ('draft','open','claimed','in_review','merged','paid','payout_failed','expired','cancelled','disputed');
CREATE TYPE claim_status AS ENUM ('active','released','expired','completed');
CREATE TYPE payout_status AS ENUM ('pending','sending','settled','failed','claim_link_issued','claimed_via_link');
CREATE TYPE deposit_status AS ENUM ('pending','settled','expired');

CREATE TABLE users (
  id               BIGSERIAL PRIMARY KEY,
  github_id        BIGINT NOT NULL UNIQUE,
  github_login     TEXT NOT NULL,
  email            TEXT,
  lightning_address TEXT,
  payout_changed_at TIMESTAMPTZ,
  is_banned        BOOLEAN NOT NULL DEFAULT FALSE,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE projects (
  id                     BIGSERIAL PRIMARY KEY,
  owner_user_id          BIGINT NOT NULL REFERENCES users(id),
  github_installation_id BIGINT NOT NULL,
  repo_full_name         TEXT NOT NULL UNIQUE,
  status                 TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','frozen','closed')),
  created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Cached balances; source of truth is ledger_entries.
CREATE TABLE wallets (
  project_id     BIGINT PRIMARY KEY REFERENCES projects(id),
  available_sats BIGINT NOT NULL DEFAULT 0 CHECK (available_sats >= 0),
  reserved_sats  BIGINT NOT NULL DEFAULT 0 CHECK (reserved_sats >= 0),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE deposits (
  id           BIGSERIAL PRIMARY KEY,
  project_id   BIGINT NOT NULL REFERENCES projects(id),
  bolt11       TEXT NOT NULL,
  payment_hash BYTEA NOT NULL UNIQUE,
  amount_sats  BIGINT NOT NULL CHECK (amount_sats > 0),
  status       deposit_status NOT NULL DEFAULT 'pending',
  idempotency_key TEXT,
  expires_at   TIMESTAMPTZ NOT NULL,
  settled_at   TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX deposits_project_idempotency_idx ON deposits(project_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE TABLE bounties (
  id                  BIGSERIAL PRIMARY KEY,
  project_id          BIGINT NOT NULL REFERENCES projects(id),
  github_issue_number INT NOT NULL,
  title               TEXT NOT NULL,
  description         TEXT NOT NULL,
  difficulty          TEXT NOT NULL CHECK (difficulty IN ('trivial','easy','medium','hard')),
  reward_sats         BIGINT NOT NULL CHECK (reward_sats >= 500),
  fee_sats            BIGINT NOT NULL CHECK (fee_sats >= 0),
  state               bounty_state NOT NULL DEFAULT 'draft',
  expires_at          TIMESTAMPTZ NOT NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (project_id, github_issue_number)
);
CREATE INDEX bounties_state_idx ON bounties(state, created_at DESC);

CREATE TABLE claims (
  id         BIGSERIAL PRIMARY KEY,
  bounty_id  BIGINT NOT NULL REFERENCES bounties(id),
  user_id    BIGINT NOT NULL REFERENCES users(id),
  status     claim_status NOT NULL DEFAULT 'active',
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Only one active claim per bounty.
CREATE UNIQUE INDEX claims_one_active_per_bounty ON claims(bounty_id) WHERE status = 'active';

CREATE TABLE pull_requests (
  id               BIGSERIAL PRIMARY KEY,
  claim_id         BIGINT NOT NULL REFERENCES claims(id),
  github_pr_number INT NOT NULL,
  author_github_id BIGINT NOT NULL,
  head_sha         TEXT,
  merged           BOOLEAN NOT NULL DEFAULT FALSE,
  merged_at        TIMESTAMPTZ,
  merge_commit_sha TEXT,
  UNIQUE (claim_id, github_pr_number)
);

CREATE TABLE payouts (
  id                 BIGSERIAL PRIMARY KEY,
  bounty_id          BIGINT NOT NULL UNIQUE REFERENCES bounties(id), -- one payout per bounty
  user_id            BIGINT NOT NULL REFERENCES users(id),
  amount_sats        BIGINT NOT NULL CHECK (amount_sats > 0),
  fee_sats           BIGINT NOT NULL CHECK (fee_sats >= 0),
  routing_fee_sats   BIGINT,
  destination        TEXT NOT NULL,
  status             payout_status NOT NULL DEFAULT 'pending',
  attempts           INT NOT NULL DEFAULT 0,
  payment_hash       BYTEA,
  last_error         TEXT,
  claim_link_token   TEXT UNIQUE,
  settled_at         TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payout_attempts (
  id           BIGSERIAL PRIMARY KEY,
  payout_id    BIGINT NOT NULL REFERENCES payouts(id),
  payment_hash BYTEA NOT NULL,
  status       TEXT NOT NULL,
  error        TEXT,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Append-only double-entry ledger. Each tx_id must sum debits = credits.
CREATE TABLE ledger_entries (
  id          BIGSERIAL PRIMARY KEY,
  tx_id       UUID NOT NULL,
  account     TEXT NOT NULL,
  direction   TEXT NOT NULL CHECK (direction IN ('debit','credit')),
  amount_sats BIGINT NOT NULL CHECK (amount_sats > 0),
  ref_type    TEXT NOT NULL,
  ref_id      BIGINT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ledger_account_idx ON ledger_entries(account);
CREATE INDEX ledger_tx_idx ON ledger_entries(tx_id);

CREATE FUNCTION ledger_no_mutation() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'ledger_entries is append-only'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER ledger_append_only BEFORE UPDATE OR DELETE ON ledger_entries
  FOR EACH ROW EXECUTE FUNCTION ledger_no_mutation();

CREATE TABLE idempotency_keys (
  key         TEXT PRIMARY KEY,
  user_id     BIGINT NOT NULL,
  request_hash TEXT NOT NULL,
  response    JSONB,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE webhook_events (
  id           BIGSERIAL PRIMARY KEY,
  delivery_id  TEXT NOT NULL UNIQUE,
  event        TEXT NOT NULL,
  payload      JSONB NOT NULL,
  processed_at TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE disputes (
  id          BIGSERIAL PRIMARY KEY,
  bounty_id   BIGINT NOT NULL REFERENCES bounties(id),
  opened_by   BIGINT NOT NULL REFERENCES users(id),
  reason      TEXT NOT NULL,
  evidence    TEXT,
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved_pay','resolved_cancel','resolved_other')),
  resolution  TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ
);

CREATE TABLE audit_log (
  id         BIGSERIAL PRIMARY KEY,
  actor      TEXT NOT NULL,
  action     TEXT NOT NULL,
  subject    TEXT NOT NULL,
  metadata   JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE waitlist (
  id         BIGSERIAL PRIMARY KEY,
  email      TEXT NOT NULL UNIQUE,
  role       TEXT CHECK (role IN ('maintainer','contributor')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;
