BEGIN;

-- GitHub account creation time, captured at sign-in for claim eligibility
-- (minimum account age rule). NULL for accounts predating this column.
ALTER TABLE users ADD COLUMN github_created_at TIMESTAMPTZ;

COMMIT;
