BEGIN;

-- Who merged the PR, for self-merge detection at payout time.
ALTER TABLE pull_requests ADD COLUMN merged_by_github_id BIGINT;

COMMIT;
