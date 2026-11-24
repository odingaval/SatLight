-- The 'expired' enum value cannot be removed (Postgres has no DROP VALUE that
-- preserves the type); it stays harmlessly while the column goes away.
ALTER TABLE payouts DROP COLUMN IF EXISTS claim_link_expires_at;
