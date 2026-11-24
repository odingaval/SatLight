BEGIN;

-- Primary repository language, fetched from GitHub at registration, so the
-- marketplace language filter matches real data instead of repo-name substrings.
ALTER TABLE projects ADD COLUMN IF NOT EXISTS language TEXT;

COMMIT;
