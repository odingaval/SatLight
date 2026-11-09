BEGIN;

ALTER TABLE pull_requests DROP COLUMN IF EXISTS merged_by_github_id;

COMMIT;
