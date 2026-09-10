DROP SEQUENCE IF EXISTS blindpulse.feed_alias_seq;
DROP INDEX IF EXISTS blindpulse.blinded_feeds_catalogue_idx;
ALTER TABLE blindpulse.blinded_feeds
    DROP COLUMN IF EXISTS trend_persistence,
    DROP COLUMN IF EXISTS realized_volatility,
    DROP COLUMN IF EXISTS built_at,
    DROP COLUMN IF EXISTS builder_version,
    DROP COLUMN IF EXISTS volume_scale;
