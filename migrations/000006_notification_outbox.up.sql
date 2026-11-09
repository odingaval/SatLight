BEGIN;

CREATE TABLE notifications (
  id BIGSERIAL PRIMARY KEY,
  kind TEXT NOT NULL,
  user_id BIGINT REFERENCES users(id),
  email TEXT,
  subject TEXT NOT NULL,
  body TEXT NOT NULL,
  ref_type TEXT NOT NULL,
  ref_id BIGINT NOT NULL,
  sent_at TIMESTAMPTZ,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (kind, ref_type, ref_id)
);

CREATE TABLE webhook_deliveries (
  id BIGSERIAL PRIMARY KEY,
  project_id BIGINT REFERENCES projects(id),
  url TEXT NOT NULL,
  event TEXT NOT NULL,
  payload JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
  attempts INT NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE projects
  ADD COLUMN notification_webhook_url TEXT,
  ADD COLUMN notification_webhook_secret TEXT;

COMMIT;
