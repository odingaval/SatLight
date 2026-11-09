BEGIN;

ALTER TABLE projects
  DROP COLUMN IF EXISTS notification_webhook_secret,
  DROP COLUMN IF EXISTS notification_webhook_url;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS notifications;

COMMIT;
