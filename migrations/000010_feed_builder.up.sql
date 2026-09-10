-- Sprint 02 additions to blinded_feeds.
ALTER TABLE blindpulse.blinded_feeds
    -- Volume is rebased for the same reason prices are: an untouched volume series is a
    -- fingerprint, and absolute share or contract counts identify a venue on their own.
    ADD COLUMN volume_scale NUMERIC(20, 10) NOT NULL DEFAULT 1 CHECK (volume_scale > 0),
    -- A normalization change must be identifiable so feeds can be rebuilt deliberately rather
    -- than silently serving two different maps of the same window.
    ADD COLUMN builder_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN built_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Cached window statistics, computed once at build time. The catalogue sorts and filters on
    -- these, and recomputing them per request would scan the bar table on every list call.
    ADD COLUMN realized_volatility NUMERIC(12, 8),
    ADD COLUMN trend_persistence NUMERIC(12, 8);

-- The catalogue's only query shape: published feeds, filtered by difficulty and timeframe.
CREATE INDEX blinded_feeds_catalogue_idx
    ON blindpulse.blinded_feeds (is_published, difficulty, base_timeframe)
    WHERE is_published = true;

-- The alias counter. A sequence rather than max()+1 so two concurrent builders cannot mint the
-- same alias, and deliberately not derived from the symbol: a hash of the ticker is reversible by
-- anyone holding a list of tickers, which is everyone.
CREATE SEQUENCE blindpulse.feed_alias_seq START 100;
