BEGIN;

CREATE TABLE destination_change_requests (
  id           BIGSERIAL PRIMARY KEY,
  user_id      BIGINT NOT NULL REFERENCES users(id),
  new_address  TEXT NOT NULL,
  token_hash   BYTEA NOT NULL UNIQUE,
  expires_at   TIMESTAMPTZ NOT NULL,
  confirmed_at TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX destination_change_requests_user_idx ON destination_change_requests(user_id);

COMMIT;
